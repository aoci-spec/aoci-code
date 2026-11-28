// Package mongocatalog reads MongoDB catalog metadata into memory.
package mongocatalog

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// ErrCatalogChanged indicates that two complete observations differed.
var ErrCatalogChanged = errors.New("MongoDB catalog changed between reads")

type queryError struct {
	command string
	cause   error
}

func (err *queryError) Error() string { return "MongoDB catalog " + err.command + " failed" }
func (err *queryError) Unwrap() error { return err.cause }

// Collection holds the raw BSON observations for one logical collection or view.
// The BSON preserves scalar encodings and the order inside index keys and pipelines.
type Collection struct {
	Name     string
	Kind     string
	Metadata bson.Raw
	Indexes  []bson.Raw

	bucketUUID bson.RawValue
}

// Collect reads the selected database twice through its catalog commands. It
// returns no observations on query errors or disagreement. Matching reads
// cannot detect a change that was reverted before the second observation.
func Collect(ctx context.Context, database *mongo.Database) ([]Collection, error) {
	first, err := readCatalog(ctx, database)
	if err != nil {
		return nil, err
	}
	second, err := readCatalog(ctx, database)
	if err != nil {
		return nil, err
	}
	if !slices.EqualFunc(first, second, equalCollection) {
		return nil, ErrCatalogChanged
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return second, nil
}

func readCatalog(ctx context.Context, database *mongo.Database) ([]Collection, error) {
	listing, err := readDocuments(ctx, database, bson.D{{Key: "listCollections", Value: 1}})
	if err != nil {
		return nil, err
	}
	if err := sortNames(listing); err != nil {
		return nil, &queryError{command: "listCollections", cause: err}
	}

	buckets := make(map[string]bson.RawValue)
	for _, item := range listing {
		name := item.Lookup("name").StringValue()
		if strings.HasPrefix(name, "system.buckets.") {
			buckets[name] = item.Lookup("info", "uuid")
		}
	}
	collections := make([]Collection, 0, len(listing))
	for _, item := range listing {
		name := item.Lookup("name").StringValue()
		if strings.HasPrefix(name, "system.") {
			continue
		}
		kind, _ := item.Lookup("type").StringValueOK()
		entry := Collection{Name: name, Kind: kind, Metadata: item}
		switch kind {
		case "view":
		case "collection", "timeseries":
			if kind == "timeseries" {
				// A logical time-series collection can omit its UUID from the
				// listing. Its bucket is a consistency guard, not an object.
				entry.bucketUUID = buckets["system.buckets."+name]
				subtype, uuid, ok := entry.bucketUUID.BinaryOK()
				if !ok || subtype != 4 || len(uuid) != 16 {
					return nil, errors.New("MongoDB time-series bucket has no UUID")
				}
			}
			entry.Indexes, err = readDocuments(ctx, database, bson.D{{Key: "listIndexes", Value: name}})
			if err != nil {
				return nil, err
			}
			if err := sortNames(entry.Indexes); err != nil {
				return nil, &queryError{command: "listIndexes", cause: err}
			}
		default:
			return nil, errors.New("MongoDB catalog returned an unsupported collection type")
		}
		collections = append(collections, entry)
	}
	return collections, nil
}

func readDocuments(ctx context.Context, database *mongo.Database, command bson.D) ([]bson.Raw, error) {
	cursor, err := database.RunCommandCursor(ctx, command)
	if err != nil {
		return nil, &queryError{command: command[0].Key, cause: err}
	}
	defer func() {
		// A canceled read can still leave a server-side cursor. Give its
		// cleanup a separate bounded context.
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		_ = cursor.Close(cleanup)
	}()
	var documents []bson.Raw
	for cursor.Next(ctx) {
		documents = append(documents, bytes.Clone(cursor.Current))
	}
	if err := cursor.Err(); err != nil {
		return nil, &queryError{command: command[0].Key, cause: err}
	}
	return documents, nil
}

func sortNames(documents []bson.Raw) error {
	for _, document := range documents {
		if name, ok := document.Lookup("name").StringValueOK(); !ok || name == "" {
			return errors.New("catalog document has no name")
		}
	}
	slices.SortFunc(documents, func(left, right bson.Raw) int {
		return strings.Compare(left.Lookup("name").StringValue(), right.Lookup("name").StringValue())
	})
	for i := 1; i < len(documents); i++ {
		if documents[i-1].Lookup("name").StringValue() == documents[i].Lookup("name").StringValue() {
			return errors.New("catalog contains a duplicate name")
		}
	}
	return nil
}

func equalCollection(left, right Collection) bool {
	return bytes.Equal(left.Metadata, right.Metadata) &&
		left.bucketUUID.Equal(right.bucketUUID) &&
		slices.EqualFunc(left.Indexes, right.Indexes, func(a, b bson.Raw) bool { return bytes.Equal(a, b) })
}
