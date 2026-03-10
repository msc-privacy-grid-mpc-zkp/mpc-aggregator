package api

import (
	"fmt"
	"sync"
)

// AggregationSession čuva podatke za jedan specifičan trenutak u vremenu
type AggregationSession struct {
	TotalSum uint64
	Count    int
	Meters   map[string]bool // Da sprečimo da jedno brojilo pošalje dvaput
}

// MemoryStore je centralna memorija servera, zaštićena Mutex-om zbog paralelnih zahteva
type MemoryStore struct {
	mu             sync.Mutex
	Sessions       map[int64]*AggregationSession
	ExpectedMeters int
}

// NewMemoryStore pravi novu memoriju i definiše koliko brojila čekamo u ulici
func NewMemoryStore(expected int) *MemoryStore {
	return &MemoryStore{
		Sessions:       make(map[int64]*AggregationSession),
		ExpectedMeters: expected,
	}
}

// AddShare dodaje udeo brojila. Vraća 'true' i prosek ako su sva brojila stigla.
func (store *MemoryStore) AddShare(timestamp int64, meterID string, share uint64) (bool, float64) {
	// Zaključavamo memoriju dok radimo sa njom da se zahtevi ne bi sudarali
	store.mu.Lock()
	defer store.mu.Unlock()

	// Ako ne postoji korpa za ovaj timestamp, pravimo je
	session, exists := store.Sessions[timestamp]
	if !exists {
		session = &AggregationSession{
			TotalSum: 0,
			Count:    0,
			Meters:   make(map[string]bool),
		}
		store.Sessions[timestamp] = session
	}

	// Ako je ovo brojilo već poslalo podatak za ovaj trenutak, ignorišemo
	if session.Meters[meterID] {
		return false, 0
	}

	// Beležimo podatak
	session.Meters[meterID] = true
	session.TotalSum += share
	session.Count++

	fmt.Printf("[AGGREGATOR] Progress for time %d: %d/%d meters\n", timestamp, session.Count, store.ExpectedMeters)

	// Ako smo skupili sva brojila, računamo prosek!
	if session.Count == store.ExpectedMeters {
		average := float64(session.TotalSum) / float64(store.ExpectedMeters)

		// Brišemo korpu da ne curi RAM memorija
		delete(store.Sessions, timestamp)

		return true, average
	}

	return false, 0
}
