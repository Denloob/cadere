package main

import (
	"html/template"
	"io"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/Denloob/cadere/engine"
)

type Templates struct {
	templates *template.Template
}

func (t *Templates) Render(w io.Writer, name string, data any, c echo.Context) error {
	return t.templates.ExecuteTemplate(w, name, data)
}

func newTemplates() *Templates {
	return &Templates{
		templates: template.Must(template.ParseGlob("templates/*.html")),
	}
}

var game = engine.NewGame(engine.NewBoard(3, 3))

type shiftFunction func(engine.Board, int) error

func shiftFunctionHandler(f shiftFunction) echo.HandlerFunc {
	return func(c echo.Context) error {
		index, err := strconv.Atoi(c.FormValue("index"))
		if err != nil {
			return c.NoContent(http.StatusBadRequest)
		}

		if err := f(game.Board, index); err != nil {
			return c.NoContent(http.StatusBadRequest)
		}
		return c.Render(http.StatusOK, "index", game)
	}
}

func main() {
	e := echo.New()
	e.Use(middleware.Logger())

	e.Renderer = newTemplates()

    game.Board[0][0] = 1
    game.Board[0][1] = 2

	e.GET("/", func(c echo.Context) error {
		return c.Render(http.StatusOK, "index", game)
	})

	e.POST("/shift/left", shiftFunctionHandler(engine.Board.ShiftLeft))
	e.POST("/shift/right", shiftFunctionHandler(engine.Board.ShiftRight))
	e.POST("/shift/up", shiftFunctionHandler(engine.Board.ShiftUp))
	e.POST("/shift/down", shiftFunctionHandler(engine.Board.ShiftDown))

	e.Logger.Fatal(e.Start(":8080"))
}
