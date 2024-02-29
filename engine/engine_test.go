package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShiftRight(t *testing.T) {
	player1 := Player(1)
	player2 := Player(2)
	game := NewGame(NewBoard(3, 1))
	game.AddPlayers(player1, player2)

	game.Board[0][0] = Tile(player1)
	game.Board[0][1] = Tile(player2)

	game.Board.ShiftRight(0)

	assert.Equal(t, tileEmpty, game.Board[0][0])
	assert.Equal(t, Tile(player1), game.Board[0][1])
	assert.Equal(t, Tile(player2), game.Board[0][2])
}

func TestShiftLeft(t *testing.T) {
	player1 := Player(1)
	player2 := Player(2)
	game := NewGame(NewBoard(3, 1))
	game.AddPlayers(player1, player2)

	game.Board[0][0] = Tile(player1)
	game.Board[0][1] = Tile(player2)

	game.Board.ShiftLeft(0)

	assert.Equal(t, Tile(player2), game.Board[0][0])
	assert.Equal(t, tileEmpty, game.Board[0][1])
	assert.Equal(t, tileEmpty, game.Board[0][2])
}

func TestShiftUp(t *testing.T) {
	player1 := Player(1)
	player2 := Player(2)
	game := NewGame(NewBoard(1, 3))
	game.AddPlayers(player1, player2)

	game.Board[0][0] = Tile(player1)
	game.Board[1][0] = Tile(player2)

	game.Board.ShiftUp(0)

	assert.Equal(t, Tile(player2), game.Board[0][0])
	assert.Equal(t, tileEmpty, game.Board[1][0])
	assert.Equal(t, tileEmpty, game.Board[2][0])
}

func TestShiftDown(t *testing.T) {
	player1 := Player(1)
	player2 := Player(2)
	game := NewGame(NewBoard(1, 3))
	game.AddPlayers(player1, player2)

	game.Board[0][0] = Tile(player1)
	game.Board[1][0] = Tile(player2)

	game.Board.ShiftDown(0)

	assert.Equal(t, tileEmpty, game.Board[0][0])
	assert.Equal(t, Tile(player1), game.Board[1][0])
	assert.Equal(t, Tile(player2), game.Board[2][0])
}
