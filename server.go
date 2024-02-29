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
		templates: template.Must(template.New("templates").Funcs(stageFuncMap).ParseGlob("templates/*.html")),
	}
}

var stageFuncMap = template.FuncMap{
	"StageInit":    func() engine.Stage { return engine.StageInit },
	"StagePlaying": func() engine.Stage { return engine.StatePlaying },
	"StageOver":    func() engine.Stage { return engine.StageOver },
}

const (
	GAME_SIZE_MAX = 100
	GAME_SIZE_MIN = 2
)

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

	e.Static("/css", "css")

	e.GET("/", func(c echo.Context) error {
		return c.Render(http.StatusOK, "index", game)
	})

	e.GET("/new", func(c echo.Context) error {
		return c.Render(http.StatusOK, "new", nil)
	})

	e.POST("/new", func(c echo.Context) error {
		size, err := strconv.Atoi(c.FormValue("size"))
		if err != nil {
			return c.Render(http.StatusUnprocessableEntity, "newForm", "The entered value is not a number")
		}
		if err != nil || size < GAME_SIZE_MIN || size > GAME_SIZE_MAX {
			return c.Render(http.StatusUnprocessableEntity, "newForm", "Board cannot be smaller than 2 or larger than 100")
		}

		game = engine.NewGame(engine.NewBoard(size, size))

		return c.Redirect(http.StatusFound, "/")
	})

	e.POST("/player/add", func(c echo.Context) error {
		playerId, errPlayerId := strconv.Atoi(c.FormValue("player"))
		if errPlayerId != nil {
			return c.NoContent(http.StatusBadRequest)
		}

		player := engine.Player(playerId)
		if game.PlayerExists(player) {
			return c.NoContent(http.StatusBadRequest)
		}

		game.AddPlayers(player)

		return c.NoContent(http.StatusOK)
	})

	e.PUT("/tile/put", func(c echo.Context) error {
		row, errRow := strconv.Atoi(c.FormValue("row"))
		col, errCol := strconv.Atoi(c.FormValue("col"))
		playerId, errPlayerId := strconv.Atoi(c.FormValue("player"))
		if errRow != nil || errCol != nil || errPlayerId != nil {
			return c.NoContent(http.StatusBadRequest)
		}

		player := engine.Player(playerId)
		if !game.PlayerExists(player) {
			return c.NoContent(http.StatusBadRequest)
		}

		if err := game.Board.Put(row, col, player.ToTile()); err != nil {
			return c.NoContent(http.StatusBadRequest)
		}

		return c.Render(http.StatusOK, "index", game)
	})

	e.POST("/shift/left", shiftFunctionHandler(engine.Board.ShiftLeft))
	e.POST("/shift/right", shiftFunctionHandler(engine.Board.ShiftRight))
	e.POST("/shift/up", shiftFunctionHandler(engine.Board.ShiftUp))
	e.POST("/shift/down", shiftFunctionHandler(engine.Board.ShiftDown))

	e.Logger.Fatal(e.Start(":8080"))
}
