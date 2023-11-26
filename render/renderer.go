package render

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"sync"
	"text/template"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/console"
	"github.com/dop251/goja_nodejs/require"
)

// Renderer is a renderer for svelte components. It uses a *goja.Runtime underneath the hood
// to run javascript.
type Renderer struct {
	template      *template.Template
	vm            *goja.Runtime
	renderfile    renderfile
	isRenderError func(goja.Value) bool
	mtx           sync.Mutex
}

// New constructs a new renderer from the given filesystem.
// The filesystem should be the "server" subdirectory of the build
// output from "npx golte".
// The second argument is the path where the JS, CSS,
// and other assets are expected to be served.
func New(fsys fs.FS) *Renderer {
	tmpl := template.Must(template.New("").ParseFS(fsys, "template.html")).Lookup("template.html")

	vm := goja.New()
	vm.SetFieldNameMapper(goja.UncapFieldNameMapper())

	require.NewRegistryWithLoader(func(path string) ([]byte, error) {
		return fs.ReadFile(fsys, path)
	}).Enable(vm)

	console.Enable(vm)

	var renderfile renderfile
	err := vm.ExportTo(require.Require(vm, "./renderfile.js"), &renderfile)
	if err != nil {
		panic(err)
	}

	var exports exports
	err = vm.ExportTo(require.Require(vm, "./exports.js"), &exports)
	if err != nil {
		panic(err)
	}

	return &Renderer{
		template:      tmpl,
		vm:            vm,
		renderfile:    renderfile,
		isRenderError: exports.IsRenderError,
	}
}

// Render renders a slice of entries into the writer
func (r *Renderer) Render(w http.ResponseWriter, components []Entry, noreload bool) error {
	if !noreload {
		r.mtx.Lock()
		result, err := r.renderfile.Render(components)
		r.mtx.Unlock()

		if err != nil {
			return r.tryConvToRenderError(err)
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")

		return r.template.Execute(w, result)
	}

	var resp []responseEntry
	for _, v := range components {
		if v.Props == nil {
			//	TODO move this logic elsewhere
			v.Props = map[string]any{}
		}

		comp := r.renderfile.Manifest[v.Comp]

		resp = append(resp, responseEntry{
			File:  "/" + comp.Client,
			Props: v.Props,
			CSS:   comp.Css,
		})
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	err := json.NewEncoder(w).Encode(resp)
	if err != nil {
		return err
	}

	return nil
}

type result struct {
	Head string
	Body string
}

type renderfile struct {
	Render   func(entries []Entry) (result, error)
	Manifest map[string]struct {
		Client string
		Css    []string
	}
}

// Entry represents a component to be rendered, along with its props.
type Entry struct {
	Comp  string
	Props map[string]any
}

type exports struct {
	IsRenderError func(goja.Value) bool
}

type responseEntry struct {
	File  string
	Props map[string]any
	CSS   []string
}