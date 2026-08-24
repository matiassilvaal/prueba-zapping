package auth

import (
	"testing"
	"time"
)

func TestNewTokenYHash(t *testing.T) {
	a, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewToken()
	if a == b || len(a) < 40 {
		t.Fatalf("tokens débiles: %q %q", a, b)
	}
	if h := HashToken(a); len(h) != 32 || string(h) == string(HashToken(b)) {
		t.Fatal("hash inesperado")
	}
}

func TestSessionCache(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewSessionCache(30*time.Second, 2)
	c.now = func() time.Time { return now }
	h1, h2, h3 := HashToken("1"), HashToken("2"), HashToken("3")

	if _, ok := c.Get(h1); ok {
		t.Fatal("miss esperado")
	}
	c.Put(h1, 7, now.Add(time.Hour))
	if id, ok := c.Get(h1); !ok || id != 7 {
		t.Fatal("hit esperado")
	}
	now = now.Add(31 * time.Second)
	if _, ok := c.Get(h1); ok {
		t.Fatal("la entrada debía vencer por TTL de caché")
	}
	c.Put(h1, 7, now.Add(5*time.Second))
	now = now.Add(6 * time.Second)
	if _, ok := c.Get(h1); ok {
		t.Fatal("la entrada debía vencer por expiración de la sesión")
	}

	c.Put(h1, 1, now.Add(time.Hour))
	c.Put(h2, 2, now.Add(time.Hour))
	c.Put(h3, 3, now.Add(time.Hour)) // supera el máximo: no se cachea
	if _, ok := c.Get(h3); ok || c.Len() != 2 {
		t.Fatal("no debía superar el máximo de entradas")
	}
	c.Delete(h1)
	if _, ok := c.Get(h1); ok {
		t.Fatal("borrado esperado")
	}
	now = now.Add(time.Minute)
	if n := c.Sweep(); n != 1 || c.Len() != 0 {
		t.Fatalf("sweep: n=%d len=%d", n, c.Len())
	}
}
