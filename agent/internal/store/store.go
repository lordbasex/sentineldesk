// SentinelDesk
// A collaborative operating system for people and AI agents.
//
// Copyright 2026 Federico Pereira <fpereira@cnsoluciones.com>
// Co-authored by Nicolas Pereira <npereira@cnsoluciones.com>
//
// Licensed under the Apache License, Version 2.0.
//
// This product's name and logo are trademarks of Federico Pereira and are not
// covered by the license above. See the README for the trademark policy.
//
// SPDX-License-Identifier: Apache-2.0

// Package store keeps what every run cost and what it was made of.
//
// Two jobs, and they are the same job seen from either end.
//
// The first is the operator's: somebody put twenty dollars on an API key and
// wants to know where it goes. Tokens are the only honest unit — a price can
// change and a stored price would then be wrong forever — so the counts are
// recorded exactly as the provider reported them and money is derived on the
// way out, from a rate the operator can correct.
//
// The second is ours: making the loop cheaper is a claim, and a claim about
// cost needs a before. Every turn records how many tools it offered and how
// many bytes of catalogue that was, because the largest line in the bill today
// is a hundred and twenty tool schemas re-sent on every single turn, and after
// the tool-selection work lands the only way to know it worked is to compare.
//
// It lives in ~/.sentineldesk/agent.db, beside the key and outside any
// checkout. It holds conversations, so it is nobody's business but the
// operator's — and it is mode 0600 for the same reason the key is.
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// DefaultPath is where the database lives unless something says otherwise.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".sentineldesk", "agent.db"), nil
}

// DB is the open database.
type DB struct {
	sql  *sql.DB
	path string
}

// Open creates the database if it is not there.
//
// A store that cannot be opened is not a reason to refuse a run. The runtime
// says so and carries on unrecorded, the same way the desktop keeps booting
// without /dev/uinput — losing the accounting is worse than not having it, and
// worse than both is a task that will not start because of a log.
func Open(path string) (*DB, error) {
	if path == "" {
		var err error
		if path, err = DefaultPath(); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	// _txlock=immediate because two runs at once are ordinary — a runtime can
	// fan sub-agents across connections — and SQLite's default deferred
	// transactions turn that into "database is locked" at the moment of the
	// write rather than at the moment of the begin.
	handle, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	db := &DB{sql: handle, path: path}
	if err := db.migrate(); err != nil {
		handle.Close()
		return nil, err
	}
	// After creation, because the file does not exist until the first write.
	_ = os.Chmod(path, 0o600)
	return db, nil
}

func (d *DB) Close() error { return d.sql.Close() }
func (d *DB) Path() string { return d.path }

const schema = `
CREATE TABLE IF NOT EXISTS runs (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  task          TEXT NOT NULL,
  goal          TEXT NOT NULL,
  model         TEXT NOT NULL,
  role          TEXT NOT NULL,
  started_at    TEXT NOT NULL,
  ended_at      TEXT,
  turns         INTEGER DEFAULT 0,
  calls         INTEGER DEFAULT 0,
  input_tokens  INTEGER DEFAULT 0,
  output_tokens INTEGER DEFAULT 0,
  cache_write   INTEGER DEFAULT 0,
  cache_read    INTEGER DEFAULT 0,
  status        TEXT,
  answer        TEXT
);

CREATE TABLE IF NOT EXISTS turns (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  run_id        INTEGER NOT NULL REFERENCES runs(id),
  n             INTEGER NOT NULL,
  at            TEXT NOT NULL,
  elapsed_ms    INTEGER,
  system        TEXT,
  request       TEXT,
  response      TEXT,
  stop          TEXT,
  input_tokens  INTEGER,
  output_tokens INTEGER,
  -- What the catalogue cost this turn. The point of recording it: the biggest
  -- line in the bill is these schemas re-sent every turn, and "we made it
  -- cheaper" needs a number from before.
  tools_offered INTEGER,
  tools_bytes   INTEGER,
  -- The prose part of the turn: the system prompt and the conversation. Kept
  -- because the catalogue's real cost is derived by subtracting this from the
  -- input count the provider reported, and that is far more honest than
  -- dividing schema bytes by four. JSON schemas tokenize badly — braces,
  -- quotes, field names — so the byte estimate said the catalogue was 56% of a
  -- turn when subtraction says 99%.
  prose_chars   INTEGER,
  -- Billed at a different rate because of caching. Separate columns because a
  -- cache that quietly stopped matching produces the same answers at ten times
  -- the price, and folding these into input_tokens would hide exactly that.
  cache_write   INTEGER DEFAULT 0,
  cache_read    INTEGER DEFAULT 0
);

CREATE TABLE IF NOT EXISTS calls (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  run_id      INTEGER NOT NULL REFERENCES runs(id),
  turn_n      INTEGER NOT NULL,
  at          TEXT NOT NULL,
  tool        TEXT NOT NULL,
  -- Set when the runtime swapped a tool for its visible twin, so the witnessed
  -- role can be audited rather than believed.
  asked_for   TEXT,
  args        TEXT,
  result      TEXT,
  denial      TEXT,
  is_error    INTEGER,
  elapsed_ms  INTEGER
);

CREATE INDEX IF NOT EXISTS turns_by_run ON turns(run_id);
CREATE INDEX IF NOT EXISTS calls_by_run ON calls(run_id);
CREATE INDEX IF NOT EXISTS runs_by_start ON runs(started_at);
`

// addedColumns are columns that arrived after the first version of the schema.
//
// CREATE TABLE IF NOT EXISTS creates a table and then does nothing forever, so
// a database made last week keeps last week's columns and every query naming a
// new one fails — which is how `history` broke the moment caching was recorded.
// Adding a column to the schema string is therefore only half the change; the
// other half is here.
//
// SQLite has no ADD COLUMN IF NOT EXISTS, so this asks what exists first. Order
// does not matter and repeating is harmless, which is what makes it safe to run
// on every open.
var addedColumns = []struct{ table, column, decl string }{
	{"turns", "prose_chars", "INTEGER"},
	{"turns", "cache_write", "INTEGER DEFAULT 0"},
	{"turns", "cache_read", "INTEGER DEFAULT 0"},
	{"runs", "cache_write", "INTEGER DEFAULT 0"},
	{"runs", "cache_read", "INTEGER DEFAULT 0"},
}

func (d *DB) migrate() error {
	if _, err := d.sql.Exec(schema); err != nil {
		return err
	}
	for _, c := range addedColumns {
		has, err := d.hasColumn(c.table, c.column)
		if err != nil {
			return err
		}
		if has {
			continue
		}
		if _, err := d.sql.Exec(
			fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", c.table, c.column, c.decl)); err != nil {
			return fmt.Errorf("adding %s.%s: %w", c.table, c.column, err)
		}
	}
	return nil
}

func (d *DB) hasColumn(table, column string) (bool, error) {
	rows, err := d.sql.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

// --- writing ------------------------------------------------------------------

// Run is one goal being worked on.
type Run struct {
	ID   int64
	db   *DB
	call int
}

// StartRun opens a record for a goal.
func (d *DB) StartRun(task, goal, model, role string) (*Run, error) {
	res, err := d.sql.Exec(
		`INSERT INTO runs (task, goal, model, role, started_at) VALUES (?,?,?,?,?)`,
		task, goal, model, role, now())
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &Run{ID: id, db: d}, nil
}

// Turn is everything about one exchange with the model.
type Turn struct {
	N                int
	Elapsed          time.Duration
	System           string
	Request          any // the messages sent, stored as JSON
	Response         any // the assistant's reply, stored as JSON
	Stop             string
	InputTokens      int
	OutputTokens     int
	ToolsOffered     int
	ToolsBytes       int
	ProseChars       int
	CacheWriteTokens int
	CacheReadTokens  int
}

// RecordTurn stores one exchange, in full.
//
// In full, deliberately. A summary answers the questions somebody thought of
// while writing the summary; the request and the response answer the ones they
// did not — why the model chose that tool, whether the system prompt said what
// it was supposed to, what the catalogue actually looked like from where the
// model sat.
func (r *Run) RecordTurn(t Turn) error {
	_, err := r.db.sql.Exec(
		`INSERT INTO turns (run_id, n, at, elapsed_ms, system, request, response,
		                    stop, input_tokens, output_tokens, tools_offered, tools_bytes,
		                    prose_chars, cache_write, cache_read)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, t.N, now(), t.Elapsed.Milliseconds(), t.System,
		asJSON(t.Request), asJSON(t.Response), t.Stop,
		t.InputTokens, t.OutputTokens, t.ToolsOffered, t.ToolsBytes, t.ProseChars,
		t.CacheWriteTokens, t.CacheReadTokens)
	return err
}

// Call is one tool invocation.
type Call struct {
	TurnN int
	Tool  string
	// AskedFor is what the model asked for when the runtime substituted
	// something else. Empty when nothing was swapped.
	AskedFor string
	Args     any
	Result   string
	Denial   string
	IsError  bool
	Elapsed  time.Duration
}

func (r *Run) RecordCall(c Call) error {
	r.call++
	_, err := r.db.sql.Exec(
		`INSERT INTO calls (run_id, turn_n, at, tool, asked_for, args, result,
		                    denial, is_error, elapsed_ms)
		 VALUES (?,?,?,?,?,?,?,?,?,?)`,
		r.ID, c.TurnN, now(), c.Tool, c.AskedFor, asJSON(c.Args),
		trunc(c.Result, 8000), c.Denial, boolInt(c.IsError), c.Elapsed.Milliseconds())
	return err
}

// Finish closes the record.
func (r *Run) Finish(status, answer string, turns, calls, in, out, cw, cr int) error {
	_, err := r.db.sql.Exec(
		`UPDATE runs SET ended_at=?, status=?, answer=?, turns=?, calls=?,
		                 input_tokens=?, output_tokens=?, cache_write=?, cache_read=?
		 WHERE id=?`,
		now(), status, answer, turns, calls, in, out, cw, cr, r.ID)
	return err
}

// --- reading ------------------------------------------------------------------

// RunSummary is one row of the ledger.
type RunSummary struct {
	ID                     int64
	Task, Goal, Model      string
	Role, Status           string
	StartedAt              string
	Turns, Calls           int
	InputTokens, OutTokens int
	CacheWrite, CacheRead  int
}

// Recent returns the last n runs, newest first.
func (d *DB) Recent(n int) ([]RunSummary, error) {
	rows, err := d.sql.Query(
		`SELECT id, task, goal, model, role, COALESCE(status,'running'), started_at,
		        turns, calls, input_tokens, output_tokens, cache_write, cache_read
		 FROM runs ORDER BY id DESC LIMIT ?`, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RunSummary
	for rows.Next() {
		var r RunSummary
		if err := rows.Scan(&r.ID, &r.Task, &r.Goal, &r.Model, &r.Role, &r.Status,
			&r.StartedAt, &r.Turns, &r.Calls, &r.InputTokens, &r.OutTokens,
			&r.CacheWrite, &r.CacheRead); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Totals is everything spent, by model.
type Totals struct {
	Model                  string
	Runs, Turns, Calls     int
	InputTokens, OutTokens int
	CacheWrite, CacheRead  int
}

func (d *DB) Totals() ([]Totals, error) {
	rows, err := d.sql.Query(
		`SELECT model, COUNT(*), COALESCE(SUM(turns),0), COALESCE(SUM(calls),0),
		        COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0),
		        COALESCE(SUM(cache_write),0), COALESCE(SUM(cache_read),0)
		 FROM runs GROUP BY model ORDER BY SUM(input_tokens) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Totals
	for rows.Next() {
		var t Totals
		if err := rows.Scan(&t.Model, &t.Runs, &t.Turns, &t.Calls,
			&t.InputTokens, &t.OutTokens, &t.CacheWrite, &t.CacheRead); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// CatalogueCost is what the tool schemas cost, which is the number the
// tool-selection work has to move.
type CatalogueCost struct {
	Turns        int
	AvgOffered   float64
	AvgInputToks float64

	// AvgProseToks is the system prompt and the conversation, estimated at four
	// characters per token — a fair rate for prose.
	AvgProseToks float64

	// AvgCatalogueToks is what is left once the prose is taken out. Derived by
	// subtraction rather than estimated from schema bytes, because that
	// estimate was wrong by a factor of two in the direction that flatters us:
	// it put the catalogue at 56% of a turn when the truth is 99%.
	AvgCatalogueToks float64
}

// Share is how much of a turn's input is catalogue.
func (c CatalogueCost) Share() float64 {
	if c.AvgInputToks <= 0 {
		return 0
	}
	return c.AvgCatalogueToks / c.AvgInputToks
}

func (d *DB) CatalogueCost() (CatalogueCost, error) {
	var c CatalogueCost
	var avgProseChars float64
	err := d.sql.QueryRow(
		`SELECT COUNT(*), COALESCE(AVG(tools_offered),0), COALESCE(AVG(input_tokens),0),
		        COALESCE(AVG(prose_chars),0)
		 FROM turns WHERE tools_offered > 0`).
		Scan(&c.Turns, &c.AvgOffered, &c.AvgInputToks, &avgProseChars)
	if err != nil {
		return c, err
	}
	c.AvgProseToks = avgProseChars / 4
	c.AvgCatalogueToks = c.AvgInputToks - c.AvgProseToks
	if c.AvgCatalogueToks < 0 {
		c.AvgCatalogueToks = 0
	}
	return c, nil
}

// TurnDetail is one exchange, for reading back.
type TurnDetail struct {
	N                      int
	At                     string
	ElapsedMS              int
	System                 string
	Request, Response      string
	Stop                   string
	InputTokens, OutTokens int
	ToolsOffered           int
}

func (d *DB) Turns(runID int64) ([]TurnDetail, error) {
	rows, err := d.sql.Query(
		`SELECT n, at, COALESCE(elapsed_ms,0), COALESCE(system,''), COALESCE(request,''),
		        COALESCE(response,''), COALESCE(stop,''), COALESCE(input_tokens,0),
		        COALESCE(output_tokens,0), COALESCE(tools_offered,0)
		 FROM turns WHERE run_id=? ORDER BY n`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TurnDetail
	for rows.Next() {
		var t TurnDetail
		if err := rows.Scan(&t.N, &t.At, &t.ElapsedMS, &t.System, &t.Request,
			&t.Response, &t.Stop, &t.InputTokens, &t.OutTokens, &t.ToolsOffered); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// CallDetail is one tool invocation, for reading back.
type CallDetail struct {
	TurnN          int
	Tool, AskedFor string
	Args, Result   string
	Denial         string
	IsError        bool
	ElapsedMS      int
}

func (d *DB) Calls(runID int64) ([]CallDetail, error) {
	rows, err := d.sql.Query(
		`SELECT turn_n, tool, COALESCE(asked_for,''), COALESCE(args,''),
		        COALESCE(result,''), COALESCE(denial,''), COALESCE(is_error,0),
		        COALESCE(elapsed_ms,0)
		 FROM calls WHERE run_id=? ORDER BY id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CallDetail
	for rows.Next() {
		var c CallDetail
		var isErr int
		if err := rows.Scan(&c.TurnN, &c.Tool, &c.AskedFor, &c.Args, &c.Result,
			&c.Denial, &isErr, &c.ElapsedMS); err != nil {
			return nil, err
		}
		c.IsError = isErr != 0
		out = append(out, c)
	}
	return out, rows.Err()
}

// --- helpers ------------------------------------------------------------------

func now() string { return time.Now().Format(time.RFC3339Nano) }

func asJSON(v any) string {
	if v == nil {
		return ""
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("(unserialisable: %v)", err)
	}
	return string(raw)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
