package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"github.com/golang-jwt/jwt"

	"github.com/Denloob/cadere/engine"
	"github.com/Denloob/cadere/util"
)

const hmacSize = 256

var hmacSecret = []byte(util.Must(GenerateNonce(hmacSize)))

type GameSession struct {
	Game  *engine.Game
	nonce string
}

func (s GameSession) Nonce() string {
	return s.nonce
}

func NewGameSession(game *engine.Game, nonce string) GameSession {
	return GameSession{
		Game:  game,
		nonce: nonce,
	}
}

func (s GameSession) NewTokenForPlayer(player engine.Player) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"nonce":  s.nonce,
		"player": player,
	})

	return token.SignedString(hmacSecret)
}

func parseToken(tokenString string) (jwt.MapClaims, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}

		return hmacSecret, nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, fmt.Errorf("invalid token")
}

func ExtractNonceFromToken(tokenString string) (string, error) {
	claims, err := parseToken(tokenString)
	if err != nil {
		return "", err
	}

	nonce, ok := claims["nonce"].(string)
	if !ok {
		return "", fmt.Errorf("invalid token")
	}

	return nonce, nil
}

func (s GameSession) ExtractPlayerFromToken(tokenString string) (engine.Player, error) {
	claims, err := parseToken(tokenString)
	if err != nil {
		return 0, err
	}

	playerId, ok := claims["player"].(float64)
	if !ok {
		return 0, fmt.Errorf("invalid token")
	}

	nonce, ok := claims["nonce"].(string)
	if !ok || nonce != s.nonce {
		return 0, fmt.Errorf("invalid token")
	}

	return engine.Player(playerId), nil
}

func GenerateNonce(bitLength int) (string, error) {
	byteLength := (bitLength + 7) / 8

	nonceBytes := make([]byte, byteLength)
	_, err := rand.Read(nonceBytes)
	if err != nil {
		return "", err
	}

	nonce := base64.URLEncoding.EncodeToString(nonceBytes)
	return nonce, nil
}
