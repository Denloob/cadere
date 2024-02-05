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

type Game struct {
	Board              Board
	players            []Player
	currentPlayerIndex int
}

func NewGame(board Board) Game {
	return Game{Board: board}
}

func (g *Game) AddPlayers(players ...Player) {
	g.players = append(g.players, players...)
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
