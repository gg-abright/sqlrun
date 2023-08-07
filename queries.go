package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

type Query struct {
	sync.Mutex
	cancel func()

	ID       uint16      `json:"id" form:"id" param:"id" query:"id"`
	DB       string      `json:"db" form:"db"`
	Sql      string      `json:"sql" form:"sql"`
	Status   QueryStatus `json:"status"`
	Results  string      `json:"results"`
	Error    error
	Started  time.Time     `json:"started"`
	Duration time.Duration `json:"duration" form:"duration"`
}

func (q *Query) Run(ctx context.Context) error {
	db, err := NewMySqlDbConn(q.DB)
	if err != nil {
		q.Error = err
		return err
	}

	sqlCtx, cancel := context.WithCancel(ctx)
	q.cancel = cancel
	q.DB = StripCreds(q.DB)

	go func() {
		defer db.Close()

		rows, err := db.QueryContext(sqlCtx, q.Sql)
		q.Lock()
		defer q.Unlock()
		fmt.Println("query exited")

		if err != nil {
			q.Error = err
			q.Status = Error
			return
		} else if rows.Err() != nil {
			q.Error = rows.Err()
			q.Status = Error
			return
		}
		defer rows.Close()
		q.Status = Success
		q.Duration = time.Now().Sub(q.Started)

		cols, _ := rows.Columns()
		result := []map[string]interface{}{}
		for rows.Next() {
			// Create a slice of interface{}'s to represent each column,
			// and a second slice to contain pointers to each item in the columns slice.
			columns := make([]interface{}, len(cols))
			columnPointers := make([]interface{}, len(cols))
			for i, _ := range columns {
				columnPointers[i] = &columns[i]
			}

			// Scan the result into the column pointers...
			rows.Scan(columnPointers...)

			// Create our map, and retrieve the value for each column from the pointers slice,
			// storing it in the map with the name of the column as the key.
			m := make(map[string]interface{})
			for i, colName := range cols {
				val := columnPointers[i].(*interface{})
				m[colName] = *val
			}

			result = append(result, m)
		}

		if len(result) > 0 {
			q.Results = fmt.Sprintf("Rows returned: %d, first record: %v", len(result), result[0])
		} else {
			q.Results = "Rows returned: 0"
		}
	}()

	return nil
}

func (q *Query) Cancel(at time.Time) {
	q.Lock()
	defer q.Unlock()

	if q.Status != Running {
		return
	}

	q.Duration = at.Sub(q.Started)
	q.Status = Canceled
}

type QueryStatus string

const (
	Running  QueryStatus = "running"
	Success              = "success"
	Canceled             = "canceled"
	Error                = "error"
)

func NewMySqlDbConn(uri string) (*sql.DB, error) {
	return sql.Open("mysql", uri)
}

func StripCreds(dsn string) string {
	parts := strings.Split(dsn, "@")
	if len(parts) == 1 {
		return parts[0]
	}

	return parts[1]
}
