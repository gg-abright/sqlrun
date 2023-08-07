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

		result := make(map[string]interface{})
		rows.Scan(&result)
		q.Results = fmt.Sprintf("Rows returned: %d", len(result))
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
