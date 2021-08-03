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
	f func()
}

func NewT() *T {
	t := &T{
		intern: &intern{"hello"},
	}

	t.f = func() {
		fmt.Println(t.v)
	}

	runtime.SetFinalizer(t.intern, func(intern *intern) {
		log.Println("finalizing t")
	})

	return t
}

func main() {
	do()

	for range time.Tick(200 * time.Millisecond) {
		runtime.GC()
	}
}

func do() {
	newT := NewT()
	newT.f()
}
