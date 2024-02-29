package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Denloob/cadere/engine"
)

func TestValidToken(t *testing.T) {
	session := NewGameSession(&engine.Game{}, "test")

	validToken, err := session.NewTokenForPlayer(1)
	assert.NoError(t, err)

	player, err := session.ExtractPlayerFromToken(validToken)

	assert.NoError(t, err)
	assert.Equal(t, engine.Player(1), player)
}

func TestInvalidToken(t *testing.T) {
	session1 := NewGameSession(&engine.Game{}, "test1")
	session2 := NewGameSession(&engine.Game{}, "test2")

	session1Token, err := session1.NewTokenForPlayer(1)
	assert.NoError(t, err)

	_, err = session2.ExtractPlayerFromToken(session1Token)

	assert.Error(t, err)
}
