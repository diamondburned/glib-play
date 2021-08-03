package main

// #cgo pkg-config: glib-2.0 gobject-2.0 gdk-pixbuf-2.0
// #include <gdk-pixbuf/gdk-pixbuf.h>
// #include <glib.h>
// #include <glib-object.h>
// extern void goMarshal(GClosure*, GValue*, guint, GValue*, gpointer, GObject*);
// extern void removeClosure(GObject*, GClosure*);
// extern void toggleNotify(gpointer, GObject*, gboolean);
import "C"

import (
	"log"
	"runtime"
	"sync"
	"unsafe"

	_ "go4.org/unsafe/assume-no-moving-gc"
)

type Object struct {
	*intern
	closures *closureRegistry
}

type intern struct {
	native *C.GObject
}

func Take(obj *C.GObject) *Object {
	C.g_object_ref(C.gpointer(unsafe.Pointer(obj)))
	return AssumeOwnership(obj)
}

func AssumeOwnership(objptr *C.GObject) *Object {
	intern := &intern{
		native: objptr,
	}
	runtime.SetFinalizer(intern, finalizeIntern)

	obj := &Object{
		intern:   intern,
		closures: makeClosures(objptr),
	}

	log.Println("new object", unsafe.Pointer(objptr))

	C.g_object_add_toggle_ref(objptr, (*[0]byte)(C.toggleNotify), nil)
	C.g_object_unref(C.gpointer(unsafe.Pointer(objptr)))

	return obj
}

func finalizeIntern(intern *intern) {
	log.Println("finalizing object", unsafe.Pointer(intern.native))

	if !checkFreeClosures(intern.native) {
		log.Println("NOT FINALIZING object", unsafe.Pointer(intern.native))
		runtime.SetFinalizer(intern, finalizeIntern)
		return
	}

	log.Println("unref object", unsafe.Pointer(intern.native))
	C.g_object_remove_toggle_ref(intern.native, (*[0]byte)(C.toggleNotify), nil)
}

//go:nocheckptr
//export goMarshal
func goMarshal(
	gclosure *C.GClosure,
	retValue *C.GValue,
	nParams C.guint,
	params *C.GValue,
	invocationHint C.gpointer,
	object *C.GObject,
) {

	var f func()
	func() {
		closures := makeClosures(object)

		fn, ok := closures.closures[gclosure]
		if !ok {
			log.Println("unknown closure ptr", gclosure)
			return
		}

		f = fn
	}()

	if f != nil {
		f()
	}
}

//go:nocheckptr
//export removeClosure
func removeClosure(object *C.GObject, gclosure *C.GClosure) {
	log.Println("removing gclosure", unsafe.Pointer(gclosure))

	shared.closureMu.Lock()
	defer shared.closureMu.Unlock()

	ui, ok := shared.closures[object]
	if !ok {
		log.Println("object", unsafe.Pointer(object), "has unknown closures")
		return
	}

	closures := (*closureRegistry)(unsafe.Pointer(ui))
	delete(closures.closures, gclosure)
}

var shared struct {
	closureMu sync.Mutex
	closures  map[*C.GObject]uintptr // -> *closures

	toggleMu sync.RWMutex
	notLast  map[*C.GObject]struct{}
}

func init() {
	shared.closures = make(map[*C.GObject]uintptr)
	shared.notLast = make(map[*C.GObject]struct{})
}

//export toggleNotify
func toggleNotify(data C.gpointer, obj *C.GObject, isLast C.gboolean) {
	log.Println("toggled object", unsafe.Pointer(obj), "isLast =", isLast != 0)

	shared.toggleMu.Lock()

	if isLast != 0 { // isLast == true
		// Go only.
		delete(shared.notLast, obj)
	} else {
		// C has it.
		shared.notLast[obj] = struct{}{}
	}

	shared.toggleMu.Unlock()

	if isLast != 0 {
		// Go only means we can run the GC.
		go runtime.GC()
	}
}

type closureRegistry struct {
	closures    map[*C.GClosure]func()
	parent      *C.GObject
	resurrected bool
}

//go:nocheckptr
func makeClosures(parent *C.GObject) *closureRegistry {
	// Query existing closures instances for the given object..
	shared.closureMu.Lock()
	defer shared.closureMu.Unlock()

	if weak, ok := shared.closures[parent]; ok {
		reg := (*closureRegistry)(unsafe.Pointer(weak))
		reg.resurrected = true
		return reg
	}

	closures := &closureRegistry{
		parent:   parent,
		closures: map[*C.GClosure]func(){},
	}

	shared.closures[parent] = uintptr(unsafe.Pointer(closures))

	return closures
}

func checkFreeClosures(parent *C.GObject) bool {
	shared.closureMu.Lock()
	defer shared.closureMu.Unlock()

	pt, ok := shared.closures[parent]
	if !ok {
		return true
	}

	closures := (*closureRegistry)(unsafe.Pointer(pt))
	if closures.resurrected {
		// GObject from Go has resurrected (C to Go) the closures. Delay
		// freeing.
		log.Println("object", unsafe.Pointer(parent), "has closures resurrected. unresurrecting it...")
		closures.resurrected = false

		// Ensure that another GC iteration is ran so we can double-check this.
		go runtime.GC()
		return false
	}

	shared.toggleMu.RLock()
	_, notLast := shared.notLast[closures.parent]
	shared.toggleMu.RUnlock()

	if notLast {
		// C still has a reference to the parent object that the closures
		// belong to, and the object might be resurrected in the Go heap.
		//
		// Don't free the closures yet; instead, delay finalizing to later.
		log.Println("object", unsafe.Pointer(parent), "not the last reference")
		return false
	}

	// C does not have a reference anymore. If this function is still being
	// called despite that, then it means the parent object is also no longer
	// used, because the parent object holds a strong reference to the closures.
	// This means we can safely remove the closures from the object registry
	// right now.
	//
	// By setting *p to a zero-value of closures, we're nilling out the map,
	// which will signal to Go that these cyclical objects can be freed
	// altogether.
	*closures = closureRegistry{}
	return true
}

func (obj *Object) ptr() unsafe.Pointer {
	return unsafe.Pointer(obj.native)
}

func (obj *Object) Connect(signal string, f func()) {
	gclosure := C.g_closure_new_simple(C.sizeof_GClosure, nil)
	obj.closures.closures[gclosure] = f

	C.g_closure_set_meta_marshal(gclosure, C.gpointer(obj.native), (*[0]byte)(C.goMarshal))
	C.g_closure_add_finalize_notifier(gclosure, C.gpointer(obj.native), (*[0]byte)(C.removeClosure))

	csig := C.CString(signal)
	defer C.free(unsafe.Pointer(csig))

	log.Println("created gclosure", unsafe.Pointer(gclosure))

	C.g_signal_connect_closure(C.gpointer(obj.ptr()), (*C.gchar)(csig), gclosure, C.FALSE)
}

type PixbufLoader struct {
	*Object
	v func()
}

func NewPixbufLoader() *PixbufLoader {
	v := C.gdk_pixbuf_loader_new()
	return &PixbufLoader{
		Object: AssumeOwnership((*C.GObject)(unsafe.Pointer(v))),
	}
}

func (l *PixbufLoader) Close() {
	C.gdk_pixbuf_loader_close((*C.GdkPixbufLoader)(unsafe.Pointer(l.native)), nil)
}

func main() {
	do()

	runtime.GC()
	select {}
}

func do() {
	// Test cases TODO:
	// - Go to C (this)
	// - C to Go

	pixbufLoader := NewPixbufLoader()
	pixbufLoader.Connect("closed", func() {
		_ = pixbufLoader // self-reference
		log.Println("signal: closed")
	})

	pixbufLoader.Close()
	log.Println("pixbufLoader closed")
}
