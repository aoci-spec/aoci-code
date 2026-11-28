//go:build integration

package mongocatalog

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/event"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestMongoDBCatalog(t *testing.T) {
	uri := os.Getenv("AOCI_TEST_MONGODB_ADMIN_URI")
	if uri == "" {
		t.Skip("set AOCI_TEST_MONGODB_ADMIN_URI to a disposable MongoDB server")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	databaseName := fmt.Sprintf("aoci_catalog_%d", time.Now().UnixNano())
	admin := catalogTestClient(t, options.Client().ApplyURI(uri))
	setup := admin.Database(databaseName)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := setup.Drop(cleanup); err != nil {
			t.Error("fixture database cleanup failed")
		}
	})

	const documentSentinel = "aoci-document-must-stay-private"
	runSetup(t, ctx, setup, bson.D{{Key: "create", Value: "menu"}})
	if _, err := setup.Collection("menu").InsertOne(ctx, bson.D{{Key: "private_field", Value: documentSentinel}}); err != nil {
		t.Fatal("fixture document insert failed")
	}
	_, err := setup.Collection("menu").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "company", Value: 1}, {Key: "service_date", Value: -1}},
		Options: options.Index().SetName("company_date").SetUnique(true).
			SetCollation(&options.Collation{Locale: "en", Strength: 2}),
	})
	if err != nil {
		t.Fatal("fixture compound index creation failed")
	}
	runSetup(t, ctx, setup, bson.D{
		{Key: "create", Value: "validated"},
		{Key: "validator", Value: bson.D{{Key: "$jsonSchema", Value: bson.D{
			{Key: "required", Value: bson.A{"amount"}},
			{Key: "properties", Value: bson.D{{Key: "amount", Value: bson.D{{Key: "minimum", Value: int64(1)}}}}},
		}}}},
	})
	runSetup(t, ctx, setup, bson.D{
		{Key: "create", Value: "validated_view"},
		{Key: "viewOn", Value: "validated"},
		{Key: "pipeline", Value: bson.A{bson.D{{Key: "$project", Value: bson.D{{Key: "amount", Value: 1}}}}}},
	})
	runSetup(t, ctx, setup, timeSeriesCommand())

	const readerPassword = "aoci-catalog-test-password"
	runSetup(t, ctx, setup, bson.D{
		{Key: "createRole", Value: "catalogOnly"},
		{Key: "privileges", Value: bson.A{bson.D{
			{Key: "resource", Value: bson.D{{Key: "db", Value: databaseName}, {Key: "collection", Value: ""}}},
			{Key: "actions", Value: bson.A{"listCollections", "listIndexes"}},
		}}},
		{Key: "roles", Value: bson.A{}},
	})
	runSetup(t, ctx, setup, bson.D{
		{Key: "createUser", Value: "reader"},
		{Key: "pwd", Value: readerPassword},
		{Key: "roles", Value: bson.A{"catalogOnly"}},
	})

	var commands []string
	var listings int
	var betweenReads func()
	reader := catalogTestClient(t, options.Client().ApplyURI(uri).
		SetAuth(options.Credential{AuthSource: databaseName, Username: "reader", Password: readerPassword}).
		SetMonitor(&event.CommandMonitor{Started: func(_ context.Context, command *event.CommandStartedEvent) {
			commands = append(commands, command.CommandName)
			if command.CommandName == "listCollections" {
				listings++
				if listings == 2 && betweenReads != nil {
					betweenReads()
				}
			}
		}})).Database(databaseName)

	t.Run("stable read with only catalog privileges", func(t *testing.T) {
		catalog, err := Collect(ctx, reader)
		if err != nil {
			t.Fatal(err)
		}
		if listings != 2 || len(catalog) != 4 || !onlyCatalogCommands(commands) {
			t.Fatalf("catalog names or command boundary mismatch: collections=%d, commands=%v", len(catalog), commands)
		}
		menu, metrics, validated, view := catalog[0], catalog[1], catalog[2], catalog[3]
		if menu.Name != "menu" || metrics.Name != "metrics" || validated.Name != "validated" || view.Name != "validated_view" {
			t.Fatal("logical collection names should be sorted and exclude system collections")
		}
		if menu.Metadata.Lookup("options", "validator").Type != 0 {
			t.Fatal("absent validator must stay absent")
		}
		if minimum, ok := validated.Metadata.Lookup("options", "validator", "$jsonSchema", "properties", "amount", "minimum").Int64OK(); !ok || minimum != 1 {
			t.Fatal("catalog lost the BSON int64 in the validator")
		}
		for _, collection := range catalog {
			for _, document := range append([]bson.Raw{collection.Metadata}, collection.Indexes...) {
				if bytes.Contains(document, []byte(documentSentinel)) || bytes.Contains(document, []byte(readerPassword)) {
					t.Fatal("catalog contains a document or credential sentinel")
				}
			}
		}
		if len(menu.Indexes) != 2 {
			t.Fatalf("expected both menu indexes, got %d", len(menu.Indexes))
		}
		compound := menu.Indexes[1]
		keys, err := compound.Lookup("key").Document().Elements()
		if err != nil || len(keys) != 2 || keys[0].Key() != "company" || keys[1].Key() != "service_date" ||
			keys[1].Value().Int32() != -1 || compound.Lookup("collation", "strength").Int32() != 2 {
			t.Fatal("catalog lost compound index order or options")
		}
		if metrics.Kind != "timeseries" || view.Kind != "view" || len(view.Indexes) != 0 ||
			view.Metadata.Lookup("options", "viewOn").StringValue() != "validated" {
			t.Fatal("catalog lost a logical time-series collection or view")
		}
	})

	t.Run("cursor pagination and permissions", func(t *testing.T) {
		commands = nil
		docs, err := readDocuments(ctx, reader, bson.D{
			{Key: "listIndexes", Value: "menu"},
			{Key: "cursor", Value: bson.D{{Key: "batchSize", Value: 1}}},
		})
		if err != nil || len(docs) != 2 || !slices.Contains(commands, "getMore") {
			t.Fatalf("metadata cursor was not exhausted: documents=%d, commands=%v, error=%v", len(docs), commands, err)
		}
		var denied mongo.CommandError
		err = reader.RunCommand(ctx, bson.D{{Key: "find", Value: "menu"}}).Err()
		if !errors.As(err, &denied) || denied.Code != 13 {
			t.Fatal("catalog role must not be able to read business documents")
		}
	})

	t.Run("index-only mutation fails the double read", func(t *testing.T) {
		listings = 0
		var setupError error
		betweenReads = func() {
			_, setupError = setup.Collection("menu").Indexes().CreateOne(ctx, mongo.IndexModel{
				Keys: bson.D{{Key: "status", Value: 1}},
			})
		}
		catalog, err := Collect(ctx, reader)
		betweenReads = nil
		if setupError != nil {
			t.Fatal("fixture index mutation failed")
		}
		if !errors.Is(err, ErrCatalogChanged) || catalog != nil {
			t.Fatalf("expected index-only change to refuse the catalog, got %v", err)
		}
	})

	t.Run("time-series replacement changes the guard", func(t *testing.T) {
		listings = 0
		var setupError error
		betweenReads = func() {
			setupError = setup.Collection("metrics").Drop(ctx)
			if setupError == nil {
				setupError = setup.RunCommand(ctx, timeSeriesCommand()).Err()
			}
		}
		catalog, err := Collect(ctx, reader)
		betweenReads = nil
		if setupError != nil {
			t.Fatal("fixture time-series replacement failed")
		}
		if !errors.Is(err, ErrCatalogChanged) || catalog != nil {
			t.Fatalf("expected time-series replacement to refuse the catalog, got %v", err)
		}
	})

	t.Run("canceled reads return no rows", func(t *testing.T) {
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		catalog, err := Collect(canceled, reader)
		if !errors.Is(err, context.Canceled) || catalog != nil {
			t.Fatalf("expected cancellation without partial catalog, got %v", err)
		}
	})

	t.Run("second pass authorization failure returns no rows", func(t *testing.T) {
		listings = 0
		var setupError error
		betweenReads = func() {
			setupError = setup.RunCommand(ctx, bson.D{
				{Key: "revokePrivilegesFromRole", Value: "catalogOnly"},
				{Key: "privileges", Value: bson.A{bson.D{
					{Key: "resource", Value: bson.D{{Key: "db", Value: databaseName}, {Key: "collection", Value: ""}}},
					{Key: "actions", Value: bson.A{"listIndexes"}},
				}}},
			}).Err()
		}
		catalog, err := Collect(ctx, reader)
		betweenReads = nil
		if setupError != nil {
			t.Fatal("fixture role update failed")
		}
		var denied mongo.CommandError
		if !errors.As(err, &denied) || denied.Code != 13 || catalog != nil ||
			err.Error() != "MongoDB catalog listIndexes failed" {
			t.Fatalf("expected a redacted second-pass error without observations, got %v", err)
		}
	})
}

func catalogTestClient(t *testing.T, config *options.ClientOptions) *mongo.Client {
	t.Helper()
	client, err := mongo.Connect(config.SetServerSelectionTimeout(5 * time.Second))
	if err != nil {
		t.Fatal("fixture MongoDB client configuration failed")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := client.Disconnect(ctx); err != nil {
			t.Error("fixture MongoDB disconnect failed")
		}
	})
	return client
}

func runSetup(t *testing.T, ctx context.Context, db *mongo.Database, command bson.D) {
	t.Helper()
	if err := db.RunCommand(ctx, command).Err(); err != nil {
		t.Fatalf("MongoDB fixture setup command %s failed", command[0].Key)
	}
}

func onlyCatalogCommands(commands []string) bool {
	for _, command := range commands {
		switch command {
		case "listCollections", "listIndexes", "getMore", "killCursors":
		default:
			return false
		}
	}
	return true
}

func timeSeriesCommand() bson.D {
	return bson.D{
		{Key: "create", Value: "metrics"},
		{Key: "timeseries", Value: bson.D{{Key: "timeField", Value: "observed_at"}, {Key: "metaField", Value: "sensor"}}},
	}
}
