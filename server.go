package main

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"strconv"
	"sync"

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
	"StageLobby":   func() engine.Stage { return engine.StageLobby },
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

type WebGameSession struct {
	socketsMutex *sync.RWMutex
	Sockets      []*websocket.Conn

	SessionMutex *sync.RWMutex
	Session      auth.GameSession
}

func NewWebGameSession(session auth.GameSession) *WebGameSession {
	return &WebGameSession{
		socketsMutex: &sync.RWMutex{},
		Sockets:      []*websocket.Conn{},

		SessionMutex: &sync.RWMutex{},
		Session:      session,
	}
}

func (w *WebGameSession) AddSocket(conn *websocket.Conn) {
	w.socketsMutex.Lock()
	defer w.socketsMutex.Unlock()

	w.Sockets = append(w.Sockets, conn)
}

func (w *WebGameSession) RemoveSocket(conn *websocket.Conn) {
	w.socketsMutex.Lock()
	defer w.socketsMutex.Unlock()

	var new_connections []*websocket.Conn
	for _, currConn := range w.Sockets {
		if currConn != conn {
			new_connections = append(new_connections, currConn)
		}
	}

	w.Sockets = new_connections
}

// FilterForEach Execute f for each element, and remove them if `f` returns false
func (w *WebGameSession) FilterForEach(f func(conn *websocket.Conn) bool) {
	w.socketsMutex.Lock()
	defer w.socketsMutex.Unlock()

	var new_connections []*websocket.Conn
	for _, currConn := range w.Sockets {

		if f(currConn) {
			new_connections = append(new_connections, currConn)
		}
	}

	w.Sockets = new_connections
}

type Games map[string]*WebGameSession

var games = make(Games)
var gamesMutex = sync.RWMutex{}

func (g Games) AddSession(session auth.GameSession) {
	gamesMutex.Lock()
	defer gamesMutex.Unlock()

	g[session.Nonce()] = NewWebGameSession(session)
}

func (g Games) GetWebSessionForToken(token string) (*WebGameSession, error) {
	gamesMutex.RLock()
	defer gamesMutex.RUnlock()

	nonce, err := auth.ExtractNonceFromToken(token)
	if err != nil {
		return nil, err
	}

	session, ok := g[nonce]
	if !ok {
		return nil, errors.New("invalid token")
	}

	return session, nil
}

type shiftFunction func(engine.Board, int) error

func (webSession WebGameSession) ExecuteAction(action GameAction, player engine.Player) (response []byte, err error) {
	webSession.SessionMutex.Lock()
	defer webSession.SessionMutex.Unlock()

	session := webSession.Session
	switch action.Action {
	case "shift":
		switch action.Direction {
		case "up":
			return shiftWith(engine.Board.ShiftUp, session, player, action.Index)
		case "down":
			return shiftWith(engine.Board.ShiftDown, session, player, action.Index)
		case "left":
			return shiftWith(engine.Board.ShiftLeft, session, player, action.Index)
		case "right":
			return shiftWith(engine.Board.ShiftRight, session, player, action.Index)
		}
	case "put":
		return putTile(session, player, action.Row, action.Col)
	case "start":
		return startSession(session, player)
	}

	return nil, fmt.Errorf("unknown action: %s", action.Action)
}

func startSession(session auth.GameSession, player engine.Player) ([]byte, error) {
	game := session.Game
	if game.Stage() != engine.StageLobby {
		return nil, fmt.Errorf("game already started")
	}

	if player != CreatorPlayerID {
		return nil, fmt.Errorf("only creator can start game")
	}

	session.Game.ProgressStage()
	return templates.RenderToBytes("gameScreen", session.Game)
}

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

	if _, err := game.Winner(); err == nil {
		game.ProgressStage()
	}

	return templates.RenderToBytes("gameScreen", game)
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

		_, cookieBytes, err := ws.ReadMessage()
		if err != nil {
			return err
		}

		cookie := string(cookieBytes)

		webSession, err := games.GetWebSessionForToken(cookie)
		if err != nil {
			return err
		}
		session := webSession.Session
		player, err := session.ExtractPlayerFromToken(cookie)
		if err != nil {
			return err
		}

		boardHTML, err := templates.RenderToBytes("gameScreen", session.Game)
		if err != nil {
			return err
		}
		err = ws.WriteMessage(websocket.TextMessage, boardHTML)
		if err != nil {
			return err
		}

		webSession.AddSocket(ws)
		defer webSession.RemoveSocket(ws)

		for {

			var action GameAction
			err := ws.ReadJSON(&action)
			if err != nil {
				continue
			}

			response, err := webSession.ExecuteAction(action, player)
			if err != nil {
				return err // TODO: render error or something
			}

			webSession.FilterForEach(func(conn *websocket.Conn) bool {
				if err = conn.WriteMessage(websocket.TextMessage, response); err == websocket.ErrCloseSent {
					return false
				}

				return true
			})
		}
	})

	e.GET("/", func(c echo.Context) error {
		cookie, err := c.Cookie(SessionCookieName)
		if err != nil {
			return c.Redirect(http.StatusFound, "/new")
		}

		webSession, err := games.GetWebSessionForToken(cookie.Value)
		if err != nil {
			return c.Redirect(http.StatusFound, "/new")
		}
		session := webSession.Session
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

		c.Response().Header().Set("HX-Redirect", "/")
		return c.NoContent(http.StatusOK)
	})

	e.GET("/join", func(c echo.Context) error {
		gameId := c.FormValue("gameId")

		webSession, ok := games[gameId]
		if !ok {
			return c.NoContent(http.StatusNotFound)
		}
		session := webSession.Session
		game := session.Game
		if game == nil {
			return c.NoContent(http.StatusInternalServerError)
		}

		if game.Stage() != engine.StageLobby {
			return c.NoContent(http.StatusBadRequest) // TODO: better error
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
