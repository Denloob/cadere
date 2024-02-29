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

const SessionCookieName = "player_id"

const (
	GAME_SIZE_MAX = 100
	GAME_SIZE_MIN = 2
)

var game *engine.Game = nil

type shiftFunction func(engine.Board, int) error

func shiftFunctionHandler(f shiftFunction) echo.HandlerFunc {
	return func(c echo.Context) error {
		if game == nil {
			return c.NoContent(http.StatusBadRequest)
		}

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
		if game == nil {
			return c.Redirect(http.StatusFound, "/new")
		}

		cookie, err := c.Cookie(SessionCookieName)
		alreadyJoinedGame := err == nil
		if !alreadyJoinedGame {
			return c.Redirect(http.StatusFound, "/join")
		}

		playerNumber, err := strconv.Atoi(cookie.Value)
		if err != nil {
			return c.Redirect(http.StatusFound, "/join")
		}

		player := engine.Player(playerNumber)
		if !game.PlayerExists(player) {
			return c.Redirect(http.StatusFound, "/join")
		}

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

		tempGame := engine.NewGame(engine.NewBoard(size, size))
		game = &tempGame

		return c.Redirect(http.StatusFound, "/")
	})

	e.GET("/join", func(c echo.Context) error {
		if game == nil {
			return c.Redirect(http.StatusFound, "/new")
		}
		if game.PlayerCount() > game.Board.MaxPlayerCount(engine.MinTilesPerPlayer) {
			return c.NoContent(http.StatusBadRequest) // TODO: return error `to many players, consider increasing board size`
		}

		playerId := game.PlayerCount() + 1
		err := game.AddPlayers(engine.Player(playerId))
		if err != nil {
			return c.NoContent(http.StatusInternalServerError)
		}

		cookie := &http.Cookie{
			Name:  SessionCookieName,
			Value: strconv.Itoa(playerId), // TODO: use a session cookie instead of a number to prevent playing as other playres
		}
		c.SetCookie(cookie)

		return c.Redirect(http.StatusFound, "/")
	})

	e.PUT("/tile/put", func(c echo.Context) error {
		if game == nil || game.Stage() != engine.StageInit {
			return c.NoContent(http.StatusBadRequest)
		}

		row, errRow := strconv.Atoi(c.FormValue("row"))
		col, errCol := strconv.Atoi(c.FormValue("col"))
		playerId, errPlayerId := strconv.Atoi(c.FormValue("player"))
		cookie, errCookie := c.Cookie(SessionCookieName)
		if errRow != nil || errCol != nil || errPlayerId != nil || errCookie != nil {
			return c.NoContent(http.StatusBadRequest)
		}

		player := engine.Player(playerId)
		if !game.PlayerExists(player) || cookie.Value != strconv.Itoa(playerId) {
			// TODO: Respond with a meaningfull error message
			return c.NoContent(http.StatusBadRequest)
		}

		if err := game.Board.Put(row, col, player.ToTile()); err != nil {
			// TODO: Respond with a meaningfull error message
			return c.NoContent(http.StatusBadRequest)
		}

		playerCount := game.PlayerCount()

		nonEmptyTileCount := game.Board.CountNonEmptyTiles()
		fullBoardNonEmptyTiles := playerCount * game.Board.TilesPerPlayerWhen(playerCount)

		if nonEmptyTileCount == fullBoardNonEmptyTiles {
			game.ProgressStage()
		}

		game.NextPlayer()
		return c.Render(http.StatusOK, "index", game)
	})

	e.POST("/shift/left", shiftFunctionHandler(engine.Board.ShiftLeft))
	e.POST("/shift/right", shiftFunctionHandler(engine.Board.ShiftRight))
	e.POST("/shift/up", shiftFunctionHandler(engine.Board.ShiftUp))
	e.POST("/shift/down", shiftFunctionHandler(engine.Board.ShiftDown))

	e.Logger.Fatal(e.Start(":8080"))
}
