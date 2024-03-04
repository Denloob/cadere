package engine

import (
	"errors"
)

type Tile int
type Player int

const (
	tileEmpty Tile = iota
	/* Anothing else is a player ID */
)

func (t Tile) IsEmpty() bool {
	return t == tileEmpty
}

func (t Tile) ToPlayer() (Player, error) {
	if t == tileEmpty {
		return 0, errors.New("tile is empty")
	}

	return Player(t), nil
}

func (t Player) ToTile() Tile {
	return Tile(t)
}

type Board [][]Tile

func NewBoard(width, height int) Board {
	b := make([][]Tile, height)
	for i := range b {
		b[i] = make([]Tile, width)
	}

	return b
}

func (b Board) validateRowIndex(row int) error {
	if row < 0 || row >= len(b) {
		return errors.New("row index out of range")
	}
	return nil
}

func (b Board) validateColIndex(col int) error {
	if col < 0 || col >= len(b[0]) {
		return errors.New("col index out of range")
	}
	return nil
}

func (b Board) Put(row, col int, tile Tile) error {
	if err := b.validateRowIndex(row); err != nil {
		return err
	}
	if err := b.validateColIndex(col); err != nil {
		return err
	}

	if !b[row][col].IsEmpty() {
		return errors.New("tile already occupied")
	}

	b[row][col] = tile
	return nil
}

func (b Board) CountNonEmptyTiles() int {
	emptyCount := 0
	for _, row := range b {
		for _, tile := range row {
			if tile.IsEmpty() {
				emptyCount++
			}
		}
	}

	boardArea := len(b) * len(b[0])
	return boardArea - emptyCount
}

func (b Board) ShiftRight(row int) error {
	if err := b.validateRowIndex(row); err != nil {
		return err
	}

	rowlen := len(b[row])
	b[row] = append([]Tile{tileEmpty}, b[row][:rowlen-1]...)
	return nil
}

func (b Board) ShiftLeft(row int) error {
	if err := b.validateRowIndex(row); err != nil {
		return err
	}

	b[row] = append(b[row][1:], tileEmpty)
	return nil
}

func (b Board) ShiftUp(col int) error {
	if err := b.validateColIndex(col); err != nil {
		return err
	}

	for row := 0; row < len(b)-1; row++ {
		b[row][col] = b[row+1][col]
	}
	b[len(b)-1][col] = tileEmpty
	return nil
}

func (b Board) ShiftDown(col int) error {
	if err := b.validateColIndex(col); err != nil {
		return err
	}

	for row := len(b) - 1; row > 0; row-- {
		b[row][col] = b[row-1][col]
	}
	b[0][col] = tileEmpty
	return nil
}

const MinTilesPerPlayer = 2
const MinPlayerCount = 1

func (b Board) TilesPerPlayerWhen(playerCount int) int {
	if playerCount > b.MaxPlayerCount(MinTilesPerPlayer) || playerCount < MinPlayerCount {
		panic("invalid amount of players")
	}

	boardArea := len(b) * len(b[0])
	return boardArea / playerCount
}

func (b Board) MaxPlayerCount(tilesPerPlayer int) int {
	if tilesPerPlayer < MinTilesPerPlayer {
		panic("too few tiles per player")
	}

	boardArea := len(b) * len(b[0])
	return boardArea / tilesPerPlayer
}

type Stage int

const (
	StageLobby Stage = iota
	StageInit
	StatePlaying
	StageOver
)

type Game struct {
	Board              Board
	stage              Stage
	players            []Player
	currentPlayerIndex int
}

func (g Game) Stage() Stage {
	return g.stage
}

func (g *Game) ProgressStage() {
	if g.stage == StageOver {
		panic("tried to progress an over game")
	}

	g.stage++
}

func (g Game) anyTilesOwnedBy(player Player) bool {
	for _, row := range g.Board {
		for _, tile := range row {
			if tile == player.ToTile() {
				return true
			}
		}
	}

	return false
}

func (g Game) Winner() (Player, error) {
	possibleWinners := []Player{}
	for _, player := range g.players {
		if g.anyTilesOwnedBy(player) {
			possibleWinners = append(possibleWinners, player)
		}
	}

	if len(possibleWinners) == 0 {
		return 0, errors.New("no winner")
	}
	if len(possibleWinners) == 1 {
		return possibleWinners[0], nil
	}
	return 0, errors.New("multiple winners")
}

func NewGame(board Board) Game {
	return Game{Board: board}
}

func (g *Game) AddPlayers(players ...Player) error {
	for _, player := range players {
		if player == 0 || g.PlayerExists(player) {
			return errors.New("invalid player")
		}
	}

	g.players = append(g.players, players...)
	return nil
}

func (g Game) PlayerCount() int {
	return len(g.players)
}

func (g Game) CurrentPlayer() Player {
	return g.players[g.currentPlayerIndex]
}

func (g *Game) NextPlayer() Player {
	g.currentPlayerIndex = (g.currentPlayerIndex + 1) % len(g.players)

	return g.CurrentPlayer()
}

func (g Game) PlayerExists(player Player) bool {
	for _, p := range g.players {
		if p == player {
			return true
		}
	}

	return false
}
