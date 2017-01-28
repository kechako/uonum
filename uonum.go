package uonum

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"strings"
	"time"

	"github.com/boltdb/bolt"
	"github.com/ikawaha/kagome/tokenizer"
	"github.com/pkg/errors"
)

var (
	bucketTexts = []byte("texts")
	bucketWords = []byte("words")
	random      = rand.New(rand.NewSource(time.Now().UnixNano()))
)

var DefaultTermWords = []string{
	"。",
	".",
}

type Generator interface {
	Open(name string) error
	Close() error

	Register(text string) error
	Generate(trigger string) (string, error)
	GenerateWithClass(trigger, class string) (string, error)
	Dump(w io.Writer) error
}

type generator struct {
	t     tokenizer.Tokenizer
	db    *bolt.DB
	twMap map[string]bool
}

func New() Generator {
	return NewWithTermWords(DefaultTermWords)
}

func NewWithTermWords(tw []string) Generator {
	twMap := make(map[string]bool)
	for _, w := range tw {
		twMap[w] = true
	}

	return &generator{
		t:     tokenizer.New(),
		twMap: twMap,
	}
}

func (g *generator) Open(name string) error {
	db, err := bolt.Open(name, 0600, nil)
	if err != nil {
		return errors.Wrap(err, "Could not open database.")
	}
	g.db = db

	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(bucketWords)
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists(bucketTexts)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return errors.Wrap(err, "Failed to create the bucket.")

	}

	return nil
}

func (g *generator) Close() error {
	if g.db == nil {
		return errors.New("Database is not opened.")
	}

	err := g.db.Close()
	if err != nil {
		err = errors.Wrap(err, "Failed to close the database.")
	}

	return err
}

type wordLink struct {
	Word     string           `json:"word"`
	Features []string         `json:"features"`
	Links    map[string]int64 `json:"links"`
}

func newWordLink(word string) *wordLink {
	return newWordLinkWithFeatures(word, nil)
}

func newWordLinkWithFeatures(word string, f []string) *wordLink {
	return &wordLink{
		Word:     word,
		Links:    make(map[string]int64),
		Features: f,
	}
}

func (w *wordLink) key() string {
	return strings.Join(
		[]string{
			w.Word,
			w.Features[0],
		}, "_")
}

func (w *wordLink) merge(other *wordLink) {
	if other == nil || other.Links == nil {
		return
	}

	for k, v := range other.Links {
		w.Links[k] += v
	}
}

func (w *wordLink) next() string {
	var total int64 = 0
	keys := make([]string, 0, len(w.Links))
	for k, c := range w.Links {
		if c == 0 {
			continue
		}
		keys = append(keys, k)
		total += c
	}
	if total == 0 {
		return ""
	}

	return keys[random.Intn(len(keys))]
}

func (g *generator) Register(text string) error {
	db := g.db
	if db == nil {
		return errors.New("Database is not opened.")
	}

	tokens := g.t.Tokenize(text)
	tokens = cleanTokens(tokens)
	if len(tokens) < 2 {
		return nil
	}

	wlmap := make(map[string]*wordLink)
	var prevwl *wordLink
	for _, t := range tokens {
		wl := newWordLinkWithFeatures(t.Surface, t.Features())
		if old, ok := wlmap[wl.key()]; ok {
			wl = old
		} else {
			wlmap[wl.key()] = wl
		}

		if prevwl != nil {
			prevwl.Links[wl.key()]++
		}

		prevwl = wl
	}

	err := db.Update(func(tx *bolt.Tx) error {
		var err error

		// put original text
		tb := tx.Bucket(bucketTexts)
		id, err := tb.NextSequence()
		if err != nil {
			return errors.Wrap(err, "Could not get next sequence.")
		}
		err = tb.Put(itob(id), []byte(text))
		if err != nil {
			return errors.Wrap(err, "Could not put text.")
		}

		b := tx.Bucket(bucketWords)

		for _, w := range wlmap {
			key := []byte(w.key())

			old := new(wordLink)
			d := b.Get(key)
			if d != nil {
				err = json.Unmarshal(d, old)
				if err != nil {
					return errors.Wrapf(err, "[%s] JSON unmarshal error.", w.Word)
				}
			}
			w.merge(old)
			d, err = json.Marshal(w)
			if err != nil {
				return errors.Wrapf(err, "[%s] JSON marshal error.", w.Word)
			}

			err = b.Put(key, d)
			if err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return errors.Wrap(err, "Failed to update the database.")
	}

	return nil
}

func cleanTokens(tokens []tokenizer.Token) []tokenizer.Token {
	c := make([]tokenizer.Token, 0, len(tokens))

	for _, t := range tokens {
		if t.ID == tokenizer.BosEosID {
			continue
		}
		if t.Surface == " " {
			continue
		}

		c = append(c, t)
	}

	return c
}

// itob returns an 8-byte big endian representation of v.
func itob(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

func (g *generator) Dump(w io.Writer) error {
	db := g.db
	if db == nil {
		return errors.New("Database is not opened.")
	}

	err := g.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketWords)

		c := b.Cursor()

		for k, v := c.First(); k != nil; k, v = c.Next() {
			wl := new(wordLink)
			err := json.Unmarshal(v, wl)
			if err != nil {
				return errors.Wrapf(err, "[%s] JSON unmarshal error.", k)
			}
			fmt.Fprintln(w, wl.key())
			for link, count := range wl.Links {
				fmt.Fprintf(w, "  %s : %d\n", link, count)
			}
			fmt.Fprintln(w)
		}

		return nil
	})
	if err != nil {
		return errors.Wrap(err, "Could not read the database.")
	}

	return nil
}

func (g *generator) Generate(trigger string) (string, error) {
	return g.GenerateWithClass(trigger, "名詞")
}

func (g *generator) GenerateWithClass(trigger, class string) (string, error) {
	if trigger == "" {
		return "", nil
	}

	db := g.db
	if db == nil {
		return "", errors.New("Database is not opened.")
	}

	buf := bytes.NewBuffer(make([]byte, 0, 4096))

	err := g.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketWords)

		key := []byte(fmt.Sprintf("%s_%s", trigger, class))
		for {
			v := b.Get(key)
			if v == nil {
				break
			}

			w := new(wordLink)
			err := json.Unmarshal(v, w)
			if err != nil {
				return errors.Wrapf(err, "[%s] JSON unmarshal error.", key)
			}

			buf.WriteString(w.Word)

			if _, ok := g.twMap[w.Word]; ok {
				break
			}

			n := w.next()
			if n == "" {
				break
			}

			key = []byte(n)
		}

		return nil
	})
	if err != nil {
		return "", errors.Wrap(err, "Could not read the database.")
	}

	return buf.String(), nil
}
