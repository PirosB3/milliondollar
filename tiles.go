package main

import "gopkg.in/redis.v4"
import "sync"
import "strconv"
import "errors"
import "time"
import "github.com/satori/go.uuid"

const (
	STATE_LOCKED_BY_OTHER        = "LOCKED_BY_OTHER"
	STATE_OPEN                   = "OPEN"
	STATE_LOCKED_BY_CURRENT_USER = "LOCKED_BY_CURRENT_USER"
	STATE_PURCHASED              = "PURCHASED"
)

type TileManager struct {
	NumTiles int
	Client   *redis.Client
	lock     sync.Mutex
        PurchaseLock sync.Mutex
}

func NewTileManager(numTiles int, client *redis.Client) *TileManager {
	return &TileManager{
		NumTiles: numTiles,
		Client:   client,
	}
}

func (tm *TileManager) KeyForBody(tile int) string {
	return "body:" + strconv.Itoa(tile)
}

func (tm *TileManager) PurchaseTile(tile int, body string, duration time.Duration) error {
	if tile >= tm.NumTiles {
		return errors.New("This tile is not available")
	}

	_, err := tm.Client.Set(
		tm.keyForTile(tile), "PURCHASED", duration,
	).Result()
        if err != nil {
            return err
        }

	_, err = tm.Client.Set(
		tm.KeyForBody(tile), body, duration,
	).Result()
        if err != nil {
            return err
        }

        return nil
}

func (tm *TileManager) Lock(tile int, duration time.Duration, locker uuid.UUID) (error, string) {
	if tile >= tm.NumTiles {
		return errors.New("This tile is not available"), ""
	}

	tm.lock.Lock()
	defer tm.lock.Unlock()

	val, err := tm.Client.SetNX(
		tm.keyForTile(tile), locker.String(), duration,
	).Result()
	if err != nil && err != redis.Nil {
		return err, ""
	}

	if val == true {
		return nil, STATE_LOCKED_BY_CURRENT_USER
	} else {
                val2, _ := tm.Client.Get(tm.keyForTile(tile)).Result()
                if val2 == "" {
                    return nil, STATE_OPEN
                } else if val2 == "PURCHASED" {
                    return nil, STATE_PURCHASED
                } else {
                    return nil, STATE_LOCKED_BY_OTHER
                }
	}
}

func (tm *TileManager) keyForTile(tile int) string {
	return "tile:" + strconv.Itoa(tile)
}

func (tm *TileManager) CanPurchase(tile int, locker uuid.UUID) (bool, error) {
    if tile >= tm.NumTiles {
            return false, errors.New("This tile is not available")
    }

    tileKey := tm.keyForTile(tile)
    val, err := tm.Client.Get(tileKey).Result()

    if err == redis.Nil {
        return false, errors.New("Tile was never locked")
    } else {
        if val != locker.String() {
            return false, errors.New("Tile was locked by someone else")
        } else {
            return true, nil
        }
    }
}

func (tm *TileManager) GetState(locker uuid.UUID) []string {
	tm.lock.Lock()
	defer tm.lock.Unlock()

	result := make([]string, tm.NumTiles)
	for i := 0; i < tm.NumTiles; i++ {
		val, err := tm.Client.Get(tm.keyForTile(i)).Result()

		if err == redis.Nil {
			result[i] = STATE_OPEN
		} else if err != nil {
			Error.Fatal(err)
		}

		if val == locker.String() {
			result[i] = STATE_LOCKED_BY_CURRENT_USER
		} else if val == "PURCHASED" {
			result[i] = STATE_PURCHASED
                } else if val == "" {
			result[i] = STATE_OPEN
		} else {
			result[i] = STATE_LOCKED_BY_OTHER
		}
	}
	return result
}
