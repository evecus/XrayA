package storage

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
)

var (
	bucketSettings = []byte("settings")
	bucketEntities = []byte("entities")
)

type DB struct{ db *bolt.DB }

type Entity struct {
	Kind      string
	ID        string
	Data      string
	UpdatedAt time.Time
}

func Open(path string) (*DB, error) {
	bdb, err := bolt.Open(path, 0600, &bolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("open bbolt %s: %w", path, err)
	}
	err = bdb.Update(func(tx *bolt.Tx) error {
		for _, b := range [][]byte{bucketSettings, bucketEntities} {
			if _, err := tx.CreateBucketIfNotExists(b); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		bdb.Close()
		return nil, fmt.Errorf("init buckets: %w", err)
	}
	return &DB{db: bdb}, nil
}

func (db *DB) Close() error { return db.db.Close() }

func (db *DB) LoadSetting(key string, v interface{}) error {
	return db.db.View(func(tx *bolt.Tx) error {
		raw := tx.Bucket(bucketSettings).Get([]byte(key))
		if raw == nil {
			return nil
		}
		return json.Unmarshal(raw, v)
	})
}

func (db *DB) SaveSetting(key string, v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal %q: %w", key, err)
	}
	return db.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketSettings).Put([]byte(key), data)
	})
}

func entityKey(kind, id string) []byte { return []byte(kind + "/" + id) }

func (db *DB) GetEntity(kind, id string) (*Entity, error) {
	var e *Entity
	err := db.db.View(func(tx *bolt.Tx) error {
		raw := tx.Bucket(bucketEntities).Get(entityKey(kind, id))
		if raw == nil {
			return nil
		}
		var row entityRow
		if err := json.Unmarshal(raw, &row); err != nil {
			return err
		}
		e = &Entity{Kind: kind, ID: id, Data: row.Data, UpdatedAt: row.UpdatedAt}
		return nil
	})
	return e, err
}

func (db *DB) ListEntities(kind string) ([]Entity, error) {
	prefix := []byte(kind + "/")
	var out []Entity
	err := db.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(bucketEntities).Cursor()
		for k, v := c.Seek(prefix); k != nil && strings.HasPrefix(string(k), string(prefix)); k, v = c.Next() {
			var row entityRow
			if err := json.Unmarshal(v, &row); err != nil {
				continue
			}
			id := strings.TrimPrefix(string(k), string(prefix))
			out = append(out, Entity{Kind: kind, ID: id, Data: row.Data, UpdatedAt: row.UpdatedAt})
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.Before(out[j].UpdatedAt) })
	return out, err
}

func (db *DB) UpsertEntity(kind, id string, v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal %s/%s: %w", kind, id, err)
	}
	row := entityRow{Data: string(data), UpdatedAt: time.Now()}
	encoded, err := json.Marshal(row)
	if err != nil {
		return err
	}
	return db.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketEntities).Put(entityKey(kind, id), encoded)
	})
}

func (db *DB) DeleteEntity(kind, id string) error {
	return db.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketEntities).Delete(entityKey(kind, id))
	})
}

func (db *DB) DeleteEntitiesByKind(kind string) error {
	prefix := []byte(kind + "/")
	return db.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketEntities)
		c := b.Cursor()
		var keys [][]byte
		for k, _ := c.Seek(prefix); k != nil && strings.HasPrefix(string(k), string(prefix)); k, _ = c.Next() {
			cp := make([]byte, len(k))
			copy(cp, k)
			keys = append(keys, cp)
		}
		for _, k := range keys {
			if err := b.Delete(k); err != nil {
				return err
			}
		}
		return nil
	})
}

// Store wraps DB for a single named settings key.
type Store struct {
	db  *DB
	key string
}

func NewStore(db *DB, key string) *Store          { return &Store{db: db, key: key} }
func (s *Store) Load(v interface{}) error          { return s.db.LoadSetting(s.key, v) }
func (s *Store) Save(v interface{}) error          { return s.db.SaveSetting(s.key, v) }

type entityRow struct {
	Data      string    `json:"d"`
	UpdatedAt time.Time `json:"t"`
}
