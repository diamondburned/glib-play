package main

import (
	"fmt"
	"log"
	"runtime"
	"time"
)

type intern struct {
	v string
}

type T struct {
	*intern
	s *signals
}

type signals struct {
	f map[int]func()
}

func NewT() *T {
	t := &T{
		intern: &intern{"hello"},
		s: &signals{
			f: make(map[int]func()),
		},
	}

	t.s.f[1] = func() {
		_ = t
		fmt.Println(t.v)
	}

	runtime.SetFinalizer(t.s, func(sigs *signals) {
		log.Println("finalizing sigs")
	})

	runtime.SetFinalizer(t.intern, func(intern *intern) {
		log.Println("finalizing t")
	})

	return t
}

func (t *T) DoThing() {
	t.s.f[1]()
}

func main() {
	do()

	for range time.Tick(200 * time.Millisecond) {
		runtime.GC()
	}
}

func do() {
	newT := NewT()
	newT.DoThing()
}
