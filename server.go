package main

import (
	"errors"
	"html/template"
	"io"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/Denloob/cadere/auth"
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

const NonceBitLength = 128
const SessionCookieName = "game"

const (
	GAME_SIZE_MAX = 100
	GAME_SIZE_MIN = 2
)

const CreatorPlayerID = 1

type Games map[string]auth.GameSession

var games = make(Games)

func (g Games) AddSession(session auth.GameSession) {
	g[session.Nonce()] = session
}

func (g Games) GetSessionForToken(token string) (auth.GameSession, error) {
	nonce, err := auth.ExtractNonceFromToken(token)
	if err != nil {
		return auth.GameSession{}, err
	}

	session, ok := g[nonce]
	if !ok {
		return auth.GameSession{}, errors.New("invalid token")
	}

	return session, nil
}

func (g Games) GetSessionForContext(c echo.Context) (auth.GameSession, error) {
	cookie, err := c.Cookie(SessionCookieName)
	if err != nil {
		return auth.GameSession{}, err
	}

	return g.GetSessionForToken(cookie.Value)
}

type shiftFunction func(engine.Board, int) error

func shiftFunctionHandler(f shiftFunction) echo.HandlerFunc {
	return func(c echo.Context) error {
		cookie, err := c.Cookie(SessionCookieName)
		if err != nil {
			return c.NoContent(http.StatusBadRequest)
		}
		session, err := games.GetSessionForToken(cookie.Value)
		if err != nil {
			return c.NoContent(http.StatusBadRequest)
		}
		game := session.Game

		if game.Stage() != engine.StatePlaying {
			return c.NoContent(http.StatusBadRequest) // TODO: Respond with a meaningfull error
		}

		player, err := session.ExtractPlayerFromToken(cookie.Value)
		if err != nil || game.CurrentPlayer() != player {
			return c.NoContent(http.StatusBadRequest) // TODO: Respond with a meaningfull error
		}

		index, err := strconv.Atoi(c.FormValue("index"))
		if err != nil {
			return c.NoContent(http.StatusBadRequest)
		}

		if err := f(game.Board, index); err != nil {
			return c.NoContent(http.StatusBadRequest)
		}

		game.NextPlayer()

		return c.Render(http.StatusOK, "index", session)
	}
}

func main() {
	e := echo.New()
	e.Use(middleware.Logger())

	e.Renderer = newTemplates()

	e.Static("/css", "css")

	e.GET("/", func(c echo.Context) error {
		cookie, err := c.Cookie(SessionCookieName)
		if err != nil {
			return nil
		}

		session, err := games.GetSessionForToken(cookie.Value)
		if err != nil {
			return c.Redirect(http.StatusFound, "/new")
		}
		game := session.Game
		if game == nil {
			return c.NoContent(http.StatusInternalServerError)
		}

		player, err := session.ExtractPlayerFromToken(cookie.Value)
		if err != nil {
			return c.Redirect(http.StatusFound, "/new")
		}

		if !game.PlayerExists(player) {
			return c.NoContent(http.StatusInternalServerError)
		}

		return c.Render(http.StatusOK, "index", session)
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

		nonce, err := auth.GenerateNonce(NonceBitLength)
		if err != nil {
			return c.NoContent(http.StatusInternalServerError)
		}

		game := engine.NewGame(engine.NewBoard(size, size))
		game.AddPlayers(CreatorPlayerID)

		session := auth.NewGameSession(&game, nonce)

		token, err := session.NewTokenForPlayer(CreatorPlayerID)
		if err != nil {
			return c.NoContent(http.StatusInternalServerError)
		}

		c.SetCookie(&http.Cookie{
			Name:  SessionCookieName,
			Value: token,
		})

		games.AddSession(session)

		return c.Redirect(http.StatusFound, "/")
	})

	e.GET("/join", func(c echo.Context) error {
		gameId := c.FormValue("gameId")

		session, ok := games[gameId]
		if !ok {
			return c.NoContent(http.StatusNotFound)
		}
		game := session.Game
		if game == nil {
			return c.NoContent(http.StatusInternalServerError)
		}

		if game.PlayerCount() > game.Board.MaxPlayerCount(engine.MinTilesPerPlayer) {
			return c.NoContent(http.StatusBadRequest) // TODO: return error `to many players, consider increasing board size`
		}

		player := engine.Player(game.PlayerCount() + 1)

		token, err := session.NewTokenForPlayer(player)
		if err != nil {
			return c.NoContent(http.StatusInternalServerError)
		}

		err = game.AddPlayers(player)
		if err != nil {
			return c.NoContent(http.StatusInternalServerError)
		}

		cookie := &http.Cookie{
			Name:  SessionCookieName,
			Value: token,
		}
		c.SetCookie(cookie)

		return c.Redirect(http.StatusFound, "/")
	})

	e.PUT("/tile/put", func(c echo.Context) error {
		session, err := games.GetSessionForContext(c)
		if err != nil || session.Game.Stage() != engine.StageInit {
			return c.NoContent(http.StatusBadRequest)
		}

		row, errRow := strconv.Atoi(c.FormValue("row"))
		col, errCol := strconv.Atoi(c.FormValue("col"))
		cookie, cookieErr := c.Cookie(SessionCookieName)
		if errRow != nil || errCol != nil || cookieErr != nil {
			return c.NoContent(http.StatusBadRequest)
		}
		player, err := session.ExtractPlayerFromToken(cookie.Value)
		if err != nil {
			return c.NoContent(http.StatusBadRequest)
		}

		game := session.Game
		if game.CurrentPlayer() != player {
			// TODO: respond with not your turn error
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
		return c.Render(http.StatusOK, "index", session)
	})

	e.POST("/shift/left", shiftFunctionHandler(engine.Board.ShiftLeft))
	e.POST("/shift/right", shiftFunctionHandler(engine.Board.ShiftRight))
	e.POST("/shift/up", shiftFunctionHandler(engine.Board.ShiftUp))
	e.POST("/shift/down", shiftFunctionHandler(engine.Board.ShiftDown))

	e.Logger.Fatal(e.Start(":8080"))
}
