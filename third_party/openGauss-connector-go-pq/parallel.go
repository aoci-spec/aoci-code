package pq

import (
	"database/sql"
	"fmt"
	"log"
	"math"
	"regexp"
	"sync"
	"strings"
)

type QueryResult struct {
	Index int
	Rows []map[string]interface{}
	Err  error                    
}

func validateQuery(query string) {
	if strings.TrimSpace(query) == "" {
		log.Fatal("invalid SQL query: must be a non-empty string")
	}

	trimmedQuery := strings.TrimSpace(query)

	selectRegex := regexp.MustCompile(`(?i)^\s*(?:--.*\s*)*SELECT\s+`)
	if !selectRegex.MatchString(trimmedQuery) {
		log.Fatal("invalid SQL query: must be a SELECT statement")
	}

	noComments := regexp.MustCompile(`--.*$`).ReplaceAllString(trimmedQuery, "")
	statements := strings.Split(noComments, ";")
	validCount := 0
	for _, stmt := range statements {
		if strings.TrimSpace(stmt) != "" {
			validCount++
		}
	}
	if validCount != 1 {
		log.Fatalf("invalid SQL query: must contain exactly one query, found %d", validCount)
	}

	vectorOpRegex := regexp.MustCompile(`<->|<=>|<#>|<\+>|<~>|<%>`)
	if !vectorOpRegex.MatchString(trimmedQuery) {
		log.Fatal("invalid SQL query: must contain vector operator <->, <=>, <+>, <~>, <%> or <#>")
	}
}


func buildSetSQL(scanParams map[string]interface{}) []string {
	if len(scanParams) == 0 {
		return nil
	}
	stmts := make([]string, 0, len(scanParams))
	for k, v := range scanParams {
		stmts = append(stmts, fmt.Sprintf("set %s=%v", k, v))
	}
	return stmts
}

func duplicate(conninfo string) (*sql.DB, error) {
	return sql.Open("opengauss", conninfo)
}

func parseRows(rows *sql.Rows) ([]map[string]interface{}, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var results []map[string]interface{}
	for rows.Next() {
		scanTargets := make([]interface{}, len(cols))
		for i := range scanTargets {
			var val interface{}
			scanTargets[i] = &val
		}

		if err := rows.Scan(scanTargets...); err != nil {
			return results, err
		}

		row := make(map[string]interface{})
		for i, col := range cols {
			val := *scanTargets[i].(*interface{})
			if b, ok := val.([]byte); ok {
				row[col] = string(b)
			} else {
				row[col] = val
			}
		}
		results = append(results, row)
	}

	return results, rows.Err()
}

func ExecuteMultiSearch(
	conninfo string,
	query string,
	args [][]interface{},
	scanParams map[string]interface{},
	threadCount int,
) ([][]map[string]interface{}) {
	validateQuery(query)

	if args == nil || len(args) == 0 {
        log.Fatal("args can not be empty")
    }

	if (threadCount <= 0) {
		log.Fatal("please confirm that the number of threads is greater than 0.")
	}

	var wg sync.WaitGroup
	resultChan := make(chan QueryResult, len(args))
	threadCount = int(math.Min(float64(len(args)), float64(threadCount)))

	var connections []*sql.DB
	for i := 0; i < threadCount; i++ {
	    newConn, err := duplicate(conninfo)
		err = newConn.Ping()
	    if err != nil {
			for _, conn := range connections {
            	if conn != nil {
					conn.Close()
				}
			}
	    	allResults := make([][]map[string]interface{}, 1)
	    	allResults[0] = []map[string]interface{}{
	    	    {
	    	        "error":   true,
	    	        "message": fmt.Sprintf("failed to create connection: %v", err),
	    	        "index":   0,
	    	    },
	    	}
	    	return allResults
		}
	    connections = append(connections, newConn)
	}

	chunkSize := (len(args) + threadCount - 1) / threadCount
	for i := 0; i < threadCount; i++ {
		start := i * chunkSize
		end := start + chunkSize
		if end > len(args) {
			end = len(args)
		}
		if start > end {
			break
		}

		wg.Add(1)
		conn := connections[i]
		go func(start, end int, conn *sql.DB) {
			defer wg.Done()
			defer conn.Close()

			setSQL := buildSetSQL(scanParams)
			if setSQL != nil {
			    for _, sql := range setSQL {
			        _, err := conn.Exec(sql)
			        if err != nil {
                        for j := start; j < end; j++ {
                            resultChan <- QueryResult{Index: j, Err: fmt.Errorf("failed to execute setSQL: %v (sql: %s)", err, sql)}
                        }
                        return
                    }
			    }

			}

            for j := start; j < end; j++ {
                arg := args[j]
                rows, err := conn.Query(query, arg...)
                if err != nil {
                    resultChan <- QueryResult{Index: j, Err: fmt.Errorf("query failed: %v", err)}
                    continue
                }

                parsedRows, err := parseRows(rows)
                rows.Close()
                if err != nil {
                    resultChan <- QueryResult{Index: j, Err: fmt.Errorf("parse failed: %v", err)}
                    continue
                }
                resultChan <- QueryResult{Index: j, Rows: parsedRows}
            }
        }(start, end, conn)
	}

	go func() {
		wg.Wait()
		close(resultChan)
	}()
	
	var allResults = make([][]map[string]interface{}, len(args))
	for res := range resultChan {
		if res.Err != nil {
			errorResult := map[string]interface{}{
                "error":   true,
                "message": res.Err.Error(),
                "index":   res.Index,
            }
            allResults[res.Index] = []map[string]interface{}{errorResult}
        } else {
            allResults[res.Index] = res.Rows
        }
	}

	return allResults
}
