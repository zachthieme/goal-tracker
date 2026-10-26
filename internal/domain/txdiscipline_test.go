package domain

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/clock"
	"github.com/zachthieme/goal-tracker/internal/db"
)

// txDisciplineService is a Service over a freshly migrated database of its
// own, for the tests that reach past the Service's commands to its queries.
func txDisciplineService(t *testing.T) *Service {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "goals.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Migrate(context.Background(), sqlDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return NewService(sqlDB, clock.NewFixed(time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)), nil, nil)
}

// A write through a Service that isn't bound to a transaction panics, naming
// the query, before it reaches the database; the same write in a transaction
// runs, and so does a read outside one.
func TestWriteOutsideTransactionPanicsWithTheQueryName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := txDisciplineService(t)

	var acc db.Account
	if err := s.WithinTx(ctx, func(tx *Service) error {
		var err error
		acc, err = tx.queries.CreateAccount(ctx, db.CreateAccountParams{Email: "ada@example.com", CreatedAt: "2026-03-02T09:00:00Z"})
		return err
	}); err != nil {
		t.Fatalf("create account in a transaction: %v", err)
	}

	name := "Ada"
	func() {
		defer func() {
			got := recover()
			if want := "write outside a transaction: SetAccountName"; got != want {
				t.Errorf("SetAccountName outside a transaction panicked with %v, want %q", got, want)
			}
		}()
		_ = s.queries.SetAccountName(ctx, db.SetAccountNameParams{Name: &name, ID: acc.ID})
	}()

	got, err := s.queries.GetAccount(ctx, acc.ID)
	if err != nil {
		t.Fatalf("read outside a transaction: %v", err)
	}
	if got.Name != nil {
		t.Errorf("the refused write reached the database: name is %q", *got.Name)
	}
}

// txDisciplineExempt are the commands that read outside the transaction they
// write in on purpose, each with why.
var txDisciplineExempt = map[string]string{
	"RestoreLinkRequest":          "an Undo spends its token in a commit of its own, so a refused restore can't roll the spending back (spendUndo)",
	"RestoreLink":                 "an Undo spends its token in a commit of its own, so a refused restore can't roll the spending back (spendUndo)",
	"RestoreHandoff":              "an Undo spends its token in a commit of its own, so a refused restore can't roll the spending back (spendUndo)",
	"UndoDeclineParentSuggestion": "an Undo spends its token in a commit of its own, so a refused restore can't roll the spending back (spendUndo)",
	"UndoRetireDimensionValue":    "an Undo spends its token in a commit of its own, so a refused restore can't roll the spending back (spendUndo)",
	"SetPassword":                 "it hashes the password outside any transaction, so the hash doesn't hold the write lock, then re-reads the link and writes in one",
	"PasswordSignIn":              "it hashes the password outside any transaction, so the hash doesn't hold the write lock, then re-reads the lockout and writes in one",
	"DeliverEmail":                "it sends outside any transaction, so a slow mail server doesn't hold the write lock, and writes each email's outcome in a short one",
	"PublishReport":               "it drafts the Report outside the transaction, so the draft's reads don't hold the write lock, then re-checks who may publish in it",
	"SyncDirectoryAsAdmin":        "it refuses a non-Admin before reading the directory, which mustn't hold the write lock, then checks again in the transaction that records it",
}

// Every command — an exported Service method that can reach a write — reads
// nothing outside the transaction it writes in, so what it checks and what it
// writes are in one transaction (#554). It reads nothing before its first
// transaction-opening call, a WithinTx or a hand-over to another command, which
// is held to the same rule; and it opens no later one once it has read since
// the last, or inside an earlier WithinTx, so a check isn't in one transaction
// and the write it guards in another. It may look someone up by email first,
// and what it reads after its last transaction has committed is its own
// business. A command in txDisciplineExempt is skipped, for the reason it gives
// there.
func TestCommandsCheckInsideTheirTransaction(t *testing.T) {
	t.Parallel()
	pkg := parseTxDisciplinePackage(t, nil)
	if len(pkg.commands()) < 50 {
		t.Fatalf("found only %d commands; is the analysis reading the package?", len(pkg.commands()))
	}
	for _, name := range pkg.commands() {
		if _, ok := txDisciplineExempt[name]; ok {
			continue
		}
		if v := pkg.walk(name); v.violation != "" {
			t.Errorf("%s checks outside the transaction it writes in: %s", name, v.violation)
		}
	}
	for name, reason := range txDisciplineExempt {
		if !slices.Contains(pkg.commands(), name) {
			t.Errorf("txDisciplineExempt names %s (%s), which is no command", name, reason)
		}
	}
}

// The analysis flags a command that checks before its transaction, however
// the check is reached, or checks after handing over to another command, or in
// one transaction and then writes in another; and passes one that checks
// inside its transaction, looks someone up by email first, hands over to
// another command, or reads after its transaction has committed.
func TestCommandsCheckInsideTheirTransactionFlagsACheckBeforeIt(t *testing.T) {
	t.Parallel()
	pkg := parseTxDisciplinePackage(t, map[string]string{"planted.go": `package domain

func (s *Service) PlantedCheckThenWrite(ctx context.Context, id int64, title string) error {
	if _, err := s.queries.GetGoal(ctx, id); err != nil {
		return err
	}
	return s.WithinTx(ctx, func(tx *Service) error {
		return tx.queries.SetAccountName(ctx, db.SetAccountNameParams{})
	})
}

func (s *Service) PlantedHelperCheckThenWrite(ctx context.Context, id int64) error {
	if err := s.plantedCheck(ctx, id); err != nil {
		return err
	}
	return s.WithinTx(ctx, func(tx *Service) error {
		return tx.queries.SetAccountName(ctx, db.SetAccountNameParams{})
	})
}

func (s *Service) plantedCheck(ctx context.Context, id int64) error {
	_, err := s.Account(ctx, id)
	return err
}

func (s *Service) PlantedWriteWithoutTx(ctx context.Context) error {
	return s.queries.SetAccountName(ctx, db.SetAccountNameParams{})
}

func (s *Service) PlantedCheckInside(ctx context.Context, id int64) error {
	return s.WithinTx(ctx, func(tx *Service) error {
		if _, err := tx.queries.GetGoal(ctx, id); err != nil {
			return err
		}
		return tx.queries.SetAccountName(ctx, db.SetAccountNameParams{})
	})
}

func (s *Service) PlantedByEmail(ctx context.Context, emailAddr string) error {
	acc, err := s.AccountByEmail(ctx, emailAddr)
	if err != nil {
		return err
	}
	return s.PlantedCheckInside(ctx, acc.ID)
}

func (s *Service) PlantedReadAfterCommit(ctx context.Context, id int64) (Account, error) {
	if err := s.PlantedCheckInside(ctx, id); err != nil {
		return Account{}, err
	}
	return s.Account(ctx, id)
}

func (s *Service) PlantedHandOverThenCheckThenWrite(ctx context.Context, id int64) error {
	if err := s.PlantedCheckInside(ctx, id); err != nil {
		return err
	}
	if _, err := s.queries.GetGoal(ctx, id); err != nil {
		return err
	}
	return s.WithinTx(ctx, func(tx *Service) error {
		return tx.queries.SetAccountName(ctx, db.SetAccountNameParams{})
	})
}

func (s *Service) PlantedCheckInOneTxWriteInAnother(ctx context.Context, id int64) error {
	if err := s.WithinTx(ctx, func(tx *Service) error {
		_, err := tx.queries.GetGoal(ctx, id)
		return err
	}); err != nil {
		return err
	}
	return s.WithinTx(ctx, func(tx *Service) error {
		return tx.queries.SetAccountName(ctx, db.SetAccountNameParams{})
	})
}

func (s *Service) PlantedReadAfterItsTx(ctx context.Context, id int64) (Account, error) {
	if err := s.WithinTx(ctx, func(tx *Service) error {
		if _, err := tx.queries.GetGoal(ctx, id); err != nil {
			return err
		}
		return tx.queries.SetAccountName(ctx, db.SetAccountNameParams{})
	}); err != nil {
		return Account{}, err
	}
	return s.Account(ctx, id)
}
`})
	for name, flagged := range map[string]bool{
		"PlantedCheckThenWrite":             true,
		"PlantedHelperCheckThenWrite":       true,
		"PlantedWriteWithoutTx":             true,
		"PlantedHandOverThenCheckThenWrite": true,
		"PlantedCheckInOneTxWriteInAnother": true,
		"PlantedCheckInside":                false,
		"PlantedByEmail":                    false,
		"PlantedReadAfterCommit":            false,
	} {
		if !slices.Contains(pkg.commands(), name) {
			t.Errorf("%s isn't found to be a command", name)
			continue
		}
		if v := pkg.walk(name); (v.violation != "") != flagged {
			t.Errorf("%s flagged = %v (%q), want %v", name, v.violation != "", v.violation, flagged)
		}
	}
}

// txDisciplinePackage is the domain package's functions as the transaction
// discipline tests read them: by name, a Service method as itself, with the
// sqlc queries that write.
type txDisciplinePackage struct {
	funcs   map[string]*ast.FuncDecl // package funcs by name
	methods map[string]*ast.FuncDecl // Service methods by name
	writes  map[string]bool          // sqlc query methods that write
	reached map[string]*bool         // method or func name: whether it reaches a write
	walked  map[string]*txDisciplineWalk
}

// txDisciplineWalk is what walking a function's calls in the order they run
// found: a violation, naming a database call made before a transaction it
// isn't in, whether it opens a transaction at all, and the read that a
// transaction its caller opens next would come after — before its first
// transaction if it opens none, or else since its last.
type txDisciplineWalk struct {
	violation string
	opensTx   bool
	read      string
}

// parseTxDisciplinePackage parses the domain package's non-test files, plus
// extra (file name to source), and the sqlc queries in internal/db.
func parseTxDisciplinePackage(t *testing.T, extra map[string]string) *txDisciplinePackage {
	t.Helper()
	fset := token.NewFileSet()
	var files []*ast.File
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	for name, src := range extra {
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	pkg := &txDisciplinePackage{
		funcs: map[string]*ast.FuncDecl{}, methods: map[string]*ast.FuncDecl{},
		writes: map[string]bool{}, reached: map[string]*bool{}, walked: map[string]*txDisciplineWalk{},
	}
	for _, f := range files {
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			switch {
			case fn.Recv == nil:
				pkg.funcs[fn.Name.Name] = fn
			case txDisciplineReceiver(fn) == "Service":
				pkg.methods[fn.Name.Name] = fn
			}
		}
	}

	queryPaths, err := filepath.Glob(filepath.Join("..", "db", "*.sql.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range queryPaths {
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			query, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatal(err)
			}
			if name, write := sqlcQueryWrites(query); name != "" && write {
				pkg.writes[name] = true
			}
			return true
		})
	}
	if len(pkg.writes) < 50 {
		t.Fatalf("found only %d writing queries in internal/db", len(pkg.writes))
	}
	return pkg
}

// txDisciplineReceiver is the type fn is a method of, pointer or not.
func txDisciplineReceiver(fn *ast.FuncDecl) string {
	typ := fn.Recv.List[0].Type
	if star, ok := typ.(*ast.StarExpr); ok {
		typ = star.X
	}
	if id, ok := typ.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// commands are the exported Service methods that can reach a write, sorted.
func (p *txDisciplinePackage) commands() []string {
	var out []string
	for name := range p.methods {
		if ast.IsExported(name) && name != "WithinTx" && p.reachesWrite(name) {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

// callee is the Service method or package func call names in this package,
// with whether it is a method, or "" for any other call.
func (p *txDisciplinePackage) callee(call *ast.CallExpr) (name string, method bool) {
	switch fun := call.Fun.(type) {
	case *ast.SelectorExpr:
		if _, ok := fun.X.(*ast.Ident); ok {
			if _, ok := p.methods[fun.Sel.Name]; ok {
				return fun.Sel.Name, true
			}
		}
	case *ast.Ident:
		if _, ok := p.funcs[fun.Name]; ok {
			return fun.Name, false
		}
	}
	return "", false
}

// txDisciplineQuery is the sqlc query call names, as s.queries.<Name>(...),
// or "".
func txDisciplineQuery(call *ast.CallExpr) string {
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		if recv, ok := sel.X.(*ast.SelectorExpr); ok && recv.Sel.Name == "queries" {
			return sel.Sel.Name
		}
	}
	return ""
}

// txDisciplineRawDB reports whether call runs SQL straight on the Service's
// *sql.DB, as s.db.<Method>(...).
func txDisciplineRawDB(call *ast.CallExpr) bool {
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		if recv, ok := sel.X.(*ast.SelectorExpr); ok && recv.Sel.Name == "db" {
			return true
		}
	}
	return false
}

// decl is the method (or else func) named name.
func (p *txDisciplinePackage) decl(name string, method bool) *ast.FuncDecl {
	if method {
		return p.methods[name]
	}
	return p.funcs[name]
}

// reachesWrite reports whether the Service method name, or a function it
// calls, closures included, calls a query that writes.
func (p *txDisciplinePackage) reachesWrite(name string) bool {
	return p.reaches(name, true, true)
}

// reaches reports whether the method or func name, or a function it calls,
// closures included, calls a query that writes, or with writesOnly false, any
// query or statement on s.db but a lookup by email.
func (p *txDisciplinePackage) reaches(name string, method, writesOnly bool) bool {
	key := txDisciplineKey(name, method) + "/" + strconv.FormatBool(writesOnly)
	if r, ok := p.reached[key]; ok {
		return r != nil && *r
	}
	p.reached[key] = nil // in progress: a recursive call adds nothing
	found := p.firstReach(p.decl(name, method).Body, writesOnly) != ""
	p.reached[key] = &found
	return found
}

// firstReach is the first call in n, closures included, that calls a query
// that writes, or with writesOnly false, any query or statement on s.db but a
// lookup by email, itself or through a function it calls; or "".
func (p *txDisciplinePackage) firstReach(n ast.Node, writesOnly bool) string {
	found := ""
	ast.Inspect(n, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || found != "" {
			return found == ""
		}
		q := txDisciplineQuery(call)
		callee, m := p.callee(call)
		switch {
		case writesOnly && q != "":
			if p.writes[q] {
				found = "queries." + q
			}
		case q == "GetAccountByEmail", m && callee == "AccountByEmail":
		case q != "":
			found = "queries." + q
		case !writesOnly && txDisciplineRawDB(call):
			found = "a statement on s.db"
		}
		if found == "" && callee != "" && p.reaches(callee, m, writesOnly) {
			found = callee
		}
		return found == ""
	})
	return found
}

func txDisciplineKey(name string, method bool) string {
	if method {
		return "Service." + name
	}
	return name
}

// walk follows the Service method name's calls in the order they run; a
// command that reads before its first transaction, or opens none, is a
// violation too.
func (p *txDisciplinePackage) walk(name string) txDisciplineWalk {
	w := p.walkFunc(name, true)
	if w.violation == "" && !w.opensTx && w.read != "" {
		w.violation = w.read + " before its first transaction"
	}
	return w
}

// walkFunc follows the calls in the body of the method or func name, each
// call's receiver and arguments before the call itself. A call that opens a
// transaction — a WithinTx, a call to a command, which is held to this rule
// itself, or a call to a function whose own walk opens one — is a violation
// when a read comes before it that isn't in a transaction it opens: a call to
// a sqlc query, a statement on s.db, or a call to a function whose walk
// reaches one, unless it looks someone up by email, since the start or the
// last transaction-opening call. A read with no transaction-opening call after
// it is the caller's to judge.
func (p *txDisciplinePackage) walkFunc(name string, method bool) txDisciplineWalk {
	key := txDisciplineKey(name, method)
	if w, ok := p.walked[key]; ok {
		if w == nil {
			return txDisciplineWalk{} // in progress: a recursive call adds nothing
		}
		return *w
	}
	p.walked[key] = nil
	var result txDisciplineWalk
	read := func(what string) {
		if result.read == "" {
			result.read = what
		}
	}
	openTx := func(readInIt string) {
		if result.read != "" {
			if result.opensTx {
				result.violation = result.read + ", then a later transaction"
			} else {
				result.violation = result.read + " before its first transaction"
			}
		}
		result.opensTx = true
		result.read = readInIt
	}
	var visit func(n ast.Node)
	visit = func(n ast.Node) {
		ast.Inspect(n, func(n ast.Node) bool {
			if result.violation != "" {
				return false
			}
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			callee, m := p.callee(call)
			if m && callee == "WithinTx" {
				readInIt := ""
				for _, arg := range call.Args {
					if r := p.firstReach(arg, false); r != "" && readInIt == "" {
						readInIt = "WithinTx → " + r
					}
				}
				openTx(readInIt)
				return false
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
				visit(sel.X)
			} else {
				visit(call.Fun)
			}
			for _, arg := range call.Args {
				visit(arg)
			}
			if result.violation != "" {
				return false
			}
			switch {
			case txDisciplineQuery(call) == "GetAccountByEmail", m && callee == "AccountByEmail":
			case txDisciplineQuery(call) != "":
				read("queries." + txDisciplineQuery(call))
			case txDisciplineRawDB(call):
				read("a statement on s.db")
			case m && ast.IsExported(callee) && p.reachesWrite(callee):
				openTx("")
			case callee != "":
				switch w := p.walkFunc(callee, m); {
				case w.violation != "":
					result.violation = callee + " → " + w.violation
				case w.opensTx:
					if w.read != "" {
						openTx(callee + " → " + w.read)
					} else {
						openTx("")
					}
				case w.read != "":
					read(callee + " → " + w.read)
				}
			}
			return false
		})
	}
	visit(p.decl(name, method).Body)
	p.walked[key] = &result
	return result
}
