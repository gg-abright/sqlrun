package main

import (
	"bytes"
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/donseba/go-htmx"
	"github.com/labstack/echo/v4"
	echoMiddleware "github.com/labstack/echo/v4/middleware"
	"golang.org/x/net/websocket"
)

func main() {
	e := echo.New()
	e.Use(echoMiddleware.Logger())
	e.Use(echoMiddleware.Recover())
	e.Use(HtmxMiddleware)

	app := &App{
		appTemplates: new(Template),
		idFactory:    IDGen(),
		state:        State{},
	}
	app.appTemplates.Add("templates/*.html")

	e.Renderer = app.appTemplates

	e.Static("/static", "../htmx/src")
	e.GET("/", app.Init)
	e.POST("/run", app.RunQuery)
	e.POST("/cancel", app.CancelQuery)

	e.GET("/queries", app.Queries)
	e.GET("/queries-update", app.QueriesUpdater)
	e.Logger.Fatal(e.Start(":3000"))
}

func IDGen() func() uint16 {
	var id uint16

	return func() uint16 {
		id = id + 1
		return id
	}
}

type App struct {
	htmx         *htmx.HTMX
	appTemplates *Template
	idFactory    func() uint16

	state State
}

func (a *App) Init(c echo.Context) error {
	return c.Render(http.StatusOK, "index.html", a.state)
}

func (a *App) RunQuery(c echo.Context) error {
	if len(a.state.Queries) == 0 {
		a.state.Queries = []*Query{}
	}

	q := &Query{
		ID:      a.idFactory(),
		Status:  Running,
		Started: time.Now(),
	}
	if err := c.Bind(q); err != nil {
		a.state.Sql = q.Sql
		a.state.Error = "unable to parse request"
	} else {
		q.Run(context.Background())
	}

	a.state.Queries = append(a.state.Queries, q)
	return c.Render(http.StatusAccepted, "app.html", a.state)
}

func (a *App) CancelQuery(c echo.Context) error {
	toCancel, err := strconv.Atoi(c.QueryParam("id"))
	if err != nil {
		return err
	}
	now := time.Now()

	for _, q := range a.state.Queries {
		if q.ID == uint16(toCancel) {
			q.Cancel(now)
		}
	}

	return nil
}

func (a *App) Queries(c echo.Context) error {
	now := time.Now()

	for i, q := range a.state.Queries {
		a.state.Queries[i].Duration = now.Sub(q.Started)
	}

	return c.Render(http.StatusOK, "queries.html", a.state)
}

func (a *App) QueriesUpdater(c echo.Context) error {
	websocket.Handler(func(ws *websocket.Conn) {
		ticker := time.NewTicker(1 * time.Second)
		buf := new(bytes.Buffer)

		defer ws.Close()
		defer ticker.Stop()
		for {
			now := <-ticker.C

			for i, q := range a.state.Queries {
				q.Lock()
				if q.Status == Running {
					a.state.Queries[i].Duration = now.Sub(q.Started)
				}
				q.Unlock()
			}
			// Write
			err := a.appTemplates.templates.ExecuteTemplate(buf, "queries_ws.html", a.state)
			if err != nil {
				c.Logger().Errorf("could not execute template: %v", err)
			}
			err = websocket.Message.Send(ws, buf.String())
			if err != nil {
				c.Logger().Errorf("could not write to websocket: %v", err)
				break
			}
			buf.Reset()
		}
	}).ServeHTTP(c.Response(), c.Request())
	return nil
}

func HtmxMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()

		hxh := htmx.HxRequestHeader{
			HxBoosted:               htmx.HxStrToBool(c.Request().Header.Get("HX-Boosted")),
			HxCurrentURL:            c.Request().Header.Get("HX-Current-URL"),
			HxHistoryRestoreRequest: htmx.HxStrToBool(c.Request().Header.Get("HX-History-Restore-Request")),
			HxPrompt:                c.Request().Header.Get("HX-Prompt"),
			HxRequest:               htmx.HxStrToBool(c.Request().Header.Get("HX-Request")),
			HxTarget:                c.Request().Header.Get("HX-Target"),
			HxTriggerName:           c.Request().Header.Get("HX-Trigger-Name"),
			HxTrigger:               c.Request().Header.Get("HX-Trigger"),
		}

		ctx = context.WithValue(ctx, htmx.ContextRequestHeader, hxh)

		c.SetRequest(c.Request().WithContext(ctx))

		return next(c)
	}
}

type State struct {
	Sql     string   `json:"sql" form:"sql"`
	Error   string   `json:"error" form:"error"`
	Queries []*Query `json:"queries" form:"queries"`
}
