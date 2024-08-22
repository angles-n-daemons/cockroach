package sql

import (
	"container/list"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cockroachdb/cockroach/pkg/sql/appstatspb"
)

type literalTtl struct {
	val string
	ttl int64
}

func NewFingerprintCardinalityCounter(ttlSeconds int64) *FingerprintCardinalityCounter {
	return &FingerprintCardinalityCounter{
		map[appstatspb.StmtFingerprintID][]*list.List{},
		ttlSeconds,
		sync.Mutex{},
	}
}

const MAX_LIST_LEN = 1000

// comment for cardinality counter
type FingerprintCardinalityCounter struct {
	lookup     map[appstatspb.StmtFingerprintID][]*list.List
	ttlSeconds int64
	mu         sync.Mutex
}

/*
 * The FingerprintCardinalityCounter is a process singleton responsible for
 * keeping track of the cardinality of string literals within various sql
 * fingerprints. It maintains an internal lookup map to do so.
 * The map is keyed by the sql fingerprint, the value of the map is an array of
 * linked lists. Each element in the array corresponds to the ith position string
 * within the fingerprint's query.
 * The elements of the linked list are the string literal values used to query.
 * example:
 *   - Fingerprint 991: SELECT _, _;
 *   - With queries: "SELECT 'hello', 'world'", "SELECT 'hello', 'moon'"
 *   Lookup = {
 *     [991]: [
 *       ['hello'], -- only hello appears for the first position
 *       ['world', 'moon'],
 *     ]
 *   }
 *
 * Note: so the linked lists do not grow indefinitely, the list is both pruned
 * by ttl and by max length.
 */
func (c *FingerprintCardinalityCounter) Add(
	fingerprintId appstatspb.StmtFingerprintID, position int, literal string,
) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now().Unix()

	l := c.get(fingerprintId, position)
	for e := l.Front(); e != nil; e = e.Next() {
		remove := false
		if e.Value.(literalTtl).ttl < now {
			remove = true
		}
		if e.Value.(literalTtl).val == literal {
			remove = true
		}

		if remove {
			l.Remove(e)
		}
	}

	if l.Len() > MAX_LIST_LEN {
		l.Remove(l.Front())
	}
	l.PushBack(literalTtl{literal, now + c.ttlSeconds})
	return l.Len()
}

func (c *FingerprintCardinalityCounter) get(
	fingerprintId appstatspb.StmtFingerprintID, position int,
) *list.List {
	if _, ok := c.lookup[fingerprintId]; !ok {
		c.lookup[fingerprintId] = []*list.List{}
	}
	for position >= len(c.lookup[fingerprintId]) {
		c.lookup[fingerprintId] = append(c.lookup[fingerprintId], list.New())
	}
	return c.lookup[fingerprintId][position]
}

func (c *FingerprintCardinalityCounter) PrettyPrint() {
	for fingerprintId, positions := range c.lookup {
		fmt.Printf("Fingerprint %s {\n", fingerprintId)
		for p := range positions {
			fmt.Printf("    Position %d: [", p)
			l := c.get(fingerprintId, p)
			vals := []string{}
			i := 0
			for e := l.Front(); e != nil; e = e.Next() {
				vals = append(vals, fmt.Sprintf("{ %s %d }", e.Value.(literalTtl).val, e.Value.(literalTtl).ttl))
				i++
			}
			fmt.Print(strings.Join(vals, " "))
			fmt.Print("]\n")

		}
		fmt.Print("}\n")
	}
}
