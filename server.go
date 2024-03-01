package main

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"strconv"

	"github.com/gorilla/websocket"
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

func (t *Templates) RenderToBytes(name string, data any) ([]byte, error) {
	var buf bytes.Buffer
	if err := t.templates.ExecuteTemplate(&buf, name, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func newTemplates() *Templates {
	return &Templates{
		templates: template.Must(template.New("templates").Funcs(stageFuncMap).ParseGlob("templates/*.html")),
	}
}

const NonceBitLength = 128
const SessionCookieName = "game"

var templates = newTemplates()

var stageFuncMap = template.FuncMap{
	"StageInit":    func() engine.Stage { return engine.StageInit },
	"StagePlaying": func() engine.Stage { return engine.StatePlaying },
	"StageOver":    func() engine.Stage { return engine.StageOver },

	"SessionCookieName": func() string { return SessionCookieName },
}

const (
	GAME_SIZE_MAX = 100
	GAME_SIZE_MIN = 2
)

const CreatorPlayerID = 1

type GameAction struct {
	Action string

	// action shift
	Index     int
	Direction string

	// action put
	Row    int
	Col    int
	Player engine.Player
}

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

func shiftWith(shiftFunc shiftFunction, session auth.GameSession, player engine.Player, index int) ([]byte, error) {
	game := session.Game

	if game.Stage() != engine.StatePlaying {
		return nil, fmt.Errorf("") // TODO: Respond with a meaningfull error
	}

	if game.CurrentPlayer() != player {
		return nil, fmt.Errorf("") // TODO: Respond with a meaningfull error
	}

	if err := shiftFunc(game.Board, index); err != nil {
		return nil, fmt.Errorf("") // TODO: Respond with a general error
	}

	game.NextPlayer()

	return templates.RenderToBytes("board", game)
}

func putTile(session auth.GameSession, player engine.Player, row, col int) ([]byte, error) {

	if session.Game.Stage() != engine.StageInit {
		return nil, fmt.Errorf("")
	}

	game := session.Game
	if game.CurrentPlayer() != player {
		// TODO: respond with not your turn error
		return nil, fmt.Errorf("")
	}

	if err := game.Board.Put(row, col, player.ToTile()); err != nil {
		// TODO: Respond with a meaningfull error message
		return nil, fmt.Errorf("")
	}

	playerCount := game.PlayerCount()

	nonEmptyTileCount := game.Board.CountNonEmptyTiles()
	fullBoardNonEmptyTiles := playerCount * game.Board.TilesPerPlayerWhen(playerCount)

	if nonEmptyTileCount == fullBoardNonEmptyTiles {
		game.ProgressStage()
	}

	game.NextPlayer()

	return templates.RenderToBytes("board", game)
}

/*
 * TODO:
 * Update all sockets of a given game when the its board state updates.
 */

var upgrader = websocket.Upgrader{}

func main() {
	e := echo.New()
	e.Use(middleware.Logger())

	e.Renderer = templates

	e.Static("/css", "css")

	e.GET("/play", func(c echo.Context) error {
		ws, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
		if err != nil {
			return err
		}
		defer ws.Close()

		var session auth.GameSession
		var player engine.Player

		_, cookieBytes, err := ws.ReadMessage()
		if err != nil {
			return err
		}

		cookie := string(cookieBytes)

		session, err = games.GetSessionForToken(cookie)
		if err != nil {
			return err
		}
		player, err = session.ExtractPlayerFromToken(cookie)
		if err != nil {
			return err
		}

		boardHTML, err := templates.RenderToBytes("board", session.Game)
		if err != nil {
			return err
		}
		err = ws.WriteMessage(websocket.TextMessage, boardHTML)
		if err != nil {
			return err
		}

		for {
			var action GameAction
			err := ws.ReadJSON(&action)
			if err != nil {
				continue
			}

			var response []byte
			err = nil
			switch action.Action {
			case "shift":
				switch action.Direction {
				case "up":
					response, err = shiftWith(engine.Board.ShiftUp, session, player, action.Index)
				case "down":
					response, err = shiftWith(engine.Board.ShiftDown, session, player, action.Index)
				case "left":
					response, err = shiftWith(engine.Board.ShiftLeft, session, player, action.Index)
				case "right":
					response, err = shiftWith(engine.Board.ShiftRight, session, player, action.Index)
				}
			case "put":
				response, err = putTile(session, player, action.Row, action.Col)
			default:
				return fmt.Errorf("unknown action: %s", action.Action)
			}

			if err != nil {
				return err // TODO: render error or something
			}

			err = ws.WriteMessage(websocket.TextMessage, response)
			if err != nil {
				return err
			}
		}
	})

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

	e.Logger.Fatal(e.Start(":8080"))
}
