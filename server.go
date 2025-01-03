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
	"sync/atomic"
	"time"

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

type GameError struct {
	error
}

func GameErrorf(format string, args ...any) error {
	return GameError{fmt.Errorf(format, args...)}
}

var (
	ErrorBadRequest      = errors.New("bad request")
	GameErrorNotYourTurn = GameErrorf("It's not your turn")
)

const NonceBitLength = 128
const SessionCookieName = "game"

const (
	GameWebsocketErrInvalidToken = "invalid token"
)

var templates = newTemplates()

var stageFuncMap = template.FuncMap{
	"StageLobby":   func() engine.Stage { return engine.StageLobby },
	"StageInit":    func() engine.Stage { return engine.StageInit },
	"StagePlaying": func() engine.Stage { return engine.StatePlaying },
	"StageOver":    func() engine.Stage { return engine.StageOver },

	"SessionCookieName": func() string { return SessionCookieName },

	"GameWebsocketErrInvalidToken": func() string { return GameWebsocketErrInvalidToken },

	"WebsocketCloseProtocolError": func() int { return websocket.CloseProtocolError },
}

const (
	GAME_SIZE_MAX = 100
	GAME_SIZE_MIN = 2

	GAME_INACTIVITY_TIMEOUT        = 10 * time.Minute
	GAME_INACTIVITY_TIMEOUT_NOTICE = 1*time.Minute + 30*time.Second
)

const CreatorPlayerID = 1

var WebsocketCloseInvalidToken = websocket.FormatCloseMessage(websocket.CloseProtocolError, GameWebsocketErrInvalidToken)

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

	lastActionTimestamp int64
}

func (session WebGameSession) LastActionTimestamp() int64 {
	return atomic.LoadInt64(&session.lastActionTimestamp)
}

func (session WebGameSession) SetLastActionTimestamp(timestamp int64) {
	atomic.StoreInt64(&session.lastActionTimestamp, timestamp)
}

func NewWebGameSession(session auth.GameSession) *WebGameSession {
	return &WebGameSession{
		socketsMutex: &sync.RWMutex{},
		Sockets:      []*websocket.Conn{},

		SessionMutex: &sync.RWMutex{},
		Session:      session,

		lastActionTimestamp: time.Now().Unix(),
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
	nonce, err := auth.ExtractNonceFromToken(token)
	if err != nil {
		return nil, err
	}

	session, ok := g.Get(nonce)
	if !ok {
		return nil, errors.New("invalid token")
	}

	return session, nil
}

func (g Games) Get(nonce string) (*WebGameSession, bool) {
	gamesMutex.RLock()
	defer gamesMutex.RUnlock()

	session, ok := g[nonce]

	return session, ok
}

func (g Games) CleanupStaleGames() {
	gamesMutex.Lock()
	defer gamesMutex.Unlock()

	currentTime := time.Now()
	for nonce, session := range g {

		lastActionTime := time.Unix(session.LastActionTimestamp(), 0)
		expirationTime := lastActionTime.Add(GAME_INACTIVITY_TIMEOUT)
		timeToExpiration := expirationTime.Sub(currentTime)

		if timeToExpiration <= GAME_INACTIVITY_TIMEOUT_NOTICE {
			expired := timeToExpiration <= 0
			notice, err := templates.RenderToBytes("expirationNotice", int64(timeToExpiration.Seconds()))

			session.FilterForEach(func(conn *websocket.Conn) bool {

				if err == nil {
					conn.WriteMessage(websocket.TextMessage, notice)
				}

				if expired {
					conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNoStatusReceived, "Stale game"))
					conn.Close()
				}

				return !expired
			})

			if expired {
				delete(g, nonce)
			}
		}
	}
}

func (g Games) CleanupStaleGamesEvery(interval time.Duration) {
	for {
		g.CleanupStaleGames()
		time.Sleep(interval)
	}
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
		return nil, GameErrorf("Game has already started")
	}

	if player != CreatorPlayerID {
		return nil, GameErrorf("Only the Host can start the game")
	}

	session.Game.ProgressStage()
	return templates.RenderToBytes("gameScreen", session.Game)
}

func shiftWith(shiftFunc shiftFunction, session auth.GameSession, player engine.Player, index int) ([]byte, error) {
	game := session.Game

	if game.Stage() != engine.StatePlaying {
		return nil, GameErrorf("The game is not in play yet")
	}

	if game.CurrentPlayer() != player {
		return nil, GameErrorNotYourTurn
	}

	if err := shiftFunc(game.Board, index); err != nil {
		return nil, ErrorBadRequest
	}

	game.NextPlayer()

	if _, err := game.Winner(); err == nil {
		game.ProgressStage()
	}

	return templates.RenderToBytes("gameScreen", game)
}

func putTile(session auth.GameSession, player engine.Player, row, col int) ([]byte, error) {

	if session.Game.Stage() != engine.StageInit {
		return nil, GameErrorf("Putting new tiles is allowed only in the init stage")
	}

	game := session.Game
	if game.CurrentPlayer() != player {
		return nil, GameErrorNotYourTurn
	}

	if err := game.Board.Put(row, col, player.ToTile()); err != nil {
		if errors.Is(err, engine.ErrorTileOccupied) {
			return nil, GameErrorf("Tile is already occupied by another player")
		}

		return nil, ErrorBadRequest
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
			ws.WriteMessage(websocket.CloseMessage, WebsocketCloseInvalidToken)
			return err
		}
		session := webSession.Session
		player, err := session.ExtractPlayerFromToken(cookie)
		if err != nil {
			ws.WriteMessage(websocket.CloseMessage, WebsocketCloseInvalidToken)
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
				if errors.Is(err, ErrorBadRequest) {
					continue
				}

				var errorPopup []byte
				var renderErr error

				switch {
				case errors.Is(err, ErrorBadRequest):
					continue
				case errors.As(err, &GameError{}):
					errorPopup, renderErr = templates.RenderToBytes("errorPopup", err.Error())
				default:
					errorPopup, renderErr = templates.RenderToBytes("errorPopup", "Something went wrong")
				}
				if renderErr != nil {
					return renderErr
				}

				if err := ws.WriteMessage(websocket.TextMessage, errorPopup); err != nil {
					return err
				}
				continue
			}

			webSession.SetLastActionTimestamp(time.Now().Unix())

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

		webSession, ok := games.Get(gameId)
		if !ok {
			return c.NoContent(http.StatusNotFound)
		}
		session := webSession.Session
		game := session.Game
		if game == nil {
			return c.NoContent(http.StatusInternalServerError)
		}

		if game.Stage() != engine.StageLobby {
			return c.Render(http.StatusUnprocessableEntity, "errorGameFull", "Game has already started")
		}

		if game.PlayerCount() > game.Board.MaxPlayerCount(engine.MinTilesPerPlayer) {
			return c.Render(http.StatusUnprocessableEntity, "errorGameFull", "Game is full")
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

	e.GET("/spectate", func(c echo.Context) error {
		return c.Render(http.StatusNotImplemented, "errorPage", "Error: Not implemented")
	})

	go games.CleanupStaleGamesEvery(time.Minute)

	e.Logger.Fatal(e.Start(":8080"))
}
