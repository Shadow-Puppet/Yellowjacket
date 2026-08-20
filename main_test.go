package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// TestMainRunsOncePerProcess pins the latch itself.
func TestMainRunsOncePerProcess(t *testing.T) {
	t.Parallel()

	mainStarted.Store(false)

	if !claimMainOnce() {
		t.Fatal("the first call to claimMainOnce must claim it")
	}

	if claimMainOnce() {
		t.Fatal("a second call to claimMainOnce must not claim it: " +
			"on Android that second call is a second main() in a live " +
			"process, and every path out of it ends in os.Exit(1)")
	}
}

// TestMainClaimsBeforeItDoesAnything is the assertion that actually
// guards #52, and it is a source sweep for the reason
// TestNoDirectRuntimeEmits is: no tier here runs main() on Android, so
// nothing else can see work creeping in above the latch.
//
// The failure it exists for is not the latch being deleted — that is
// loud. It is a line being added above it: a second
// NewYellowJacketApp opens the SQLite database a second time in one
// process, and it would do so on every activity recreation, silently,
// on a build that otherwise looks entirely healthy.
func TestMainClaimsBeforeItDoesAnything(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}

	var fn *ast.FuncDecl

	for _, decl := range file.Decls {
		d, ok := decl.(*ast.FuncDecl)
		if ok && d.Name.Name == "main" && d.Recv == nil {
			fn = d

			break
		}
	}

	if fn == nil {
		t.Fatal("no func main in main.go — this test read the wrong file")
	}

	if len(fn.Body.List) == 0 {
		t.Fatal("func main is empty")
	}

	if !claimsMainOnce(fn.Body.List[0]) {
		t.Fatalf("the first statement of main() must be the "+
			"`if !claimMainOnce() { return }` guard, got %T — see #52: "+
			"Android calls main() once per activity, in a process that "+
			"outlives the activity, so anything above the guard runs "+
			"again on every recreation", fn.Body.List[0])
	}
}

// claimsMainOnce reports whether stmt is `if !claimMainOnce() { return }`.
func claimsMainOnce(stmt ast.Stmt) bool {
	ifStmt, ok := stmt.(*ast.IfStmt)
	if !ok {
		return false
	}

	unary, ok := ifStmt.Cond.(*ast.UnaryExpr)
	if !ok || unary.Op != token.NOT {
		return false
	}

	call, ok := unary.X.(*ast.CallExpr)
	if !ok {
		return false
	}

	ident, ok := call.Fun.(*ast.Ident)
	if !ok || ident.Name != "claimMainOnce" {
		return false
	}

	if len(ifStmt.Body.List) != 1 {
		return false
	}

	_, ok = ifStmt.Body.List[0].(*ast.ReturnStmt)

	return ok
}
