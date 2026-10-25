package dynamodb

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"webtuidb/internal/db"
)

// maxTransaction is DynamoDB's limit of statements in one ExecuteTransaction.
const maxTransaction = 100

var fromRe = regexp.MustCompile(`(?is)\bFROM\s+("(?:[^"]|"")+"|[\w.\-]+)`)

// Exec runs ONE PartiQL statement (or one of the table-level statements, see the package comment).
// limit > 0 stops the read once more than limit items arrived, which sets Truncated.
func (s *session) Exec(ctx context.Context, stmt string, limit int) (*db.Result, error) {
	start := time.Now()
	text := stripLead(stmt)
	if text == "" {
		return &db.Result{Message: "empty statement"}, nil
	}
	a, err := parseAdmin(text)
	if err != nil {
		return nil, err
	}
	var res *db.Result
	if a != nil {
		msg, err := s.runAdmin(ctx, a, false)
		if err != nil {
			return nil, err
		}
		res = &db.Result{Message: msg}
	} else if res, err = s.execPartiQL(ctx, text, limit); err != nil {
		return nil, err
	}
	res.Duration = time.Since(start)
	return res, nil
}

func (s *session) execPartiQL(ctx context.Context, text string, limit int) (*db.Result, error) {
	isSelect := strings.EqualFold((&scanner{s: text}).word(), "SELECT")
	var items []map[string]types.AttributeValue
	var token *string
	for {
		in := &dynamodb.ExecuteStatementInput{Statement: aws.String(text), NextToken: token}
		if limit > 0 { // Limit counts items evaluated, so a filtered read needs room to find matches
			in.Limit = aws.Int32(int32(max(limit+1-len(items), 100)))
		}
		out, err := s.client().ExecuteStatement(ctx, in)
		if err != nil {
			return nil, wrap("PartiQL", err)
		}
		items = append(items, out.Items...)
		if token = out.NextToken; token == nil || (limit > 0 && len(items) > limit) {
			break
		}
	}
	trunc := limit > 0 && len(items) > limit
	if trunc {
		items = items[:limit]
	}
	if !isSelect && len(items) == 0 {
		return &db.Result{Message: "statement executed (DynamoDB does not report affected rows)"}, nil
	}
	// the table's key attributes lead the columns; its structure is read once when it is not cached yet
	var m *tableMeta
	if f := fromRe.FindStringSubmatch(text); f != nil {
		table := strings.ReplaceAll(strings.Trim(f[1], `"`), `""`, `"`)
		s.mu.Lock()
		m = s.metas[table]
		s.mu.Unlock()
		if m == nil && len(items) > 0 {
			if d, err := s.describe(ctx, table); err == nil {
				m = metaOf(d)
			}
		}
	}
	res := render(items, m)
	res.Truncated = trunc
	return res, nil
}

// render builds a result from items: the table's key attributes first when it is known, the other
// attributes alphabetically.
func render(items []map[string]types.AttributeValue, m *tableMeta) *db.Result {
	seen := map[string]map[string]bool{}
	for _, it := range items {
		for n, v := range it {
			if seen[n] == nil {
				seen[n] = map[string]bool{}
			}
			seen[n][typeName(v)] = true
		}
	}
	var names []string
	if m != nil {
		for _, k := range m.keys() {
			if seen[k] != nil {
				names = append(names, k)
			}
		}
	}
	first := len(names)
	for n := range seen {
		if m == nil || !m.isKey(n) {
			names = append(names, n)
		}
	}
	sort.Strings(names[first:])
	res := &db.Result{HasRows: true}
	for _, n := range names {
		ts := seen[n]
		if len(ts) > 1 {
			delete(ts, "NULL")
		}
		t := "mixed"
		for k := range ts {
			if len(ts) == 1 {
				t = k
			}
		}
		res.Columns = append(res.Columns, db.ResultColumn{Name: n, Type: t})
	}
	for _, it := range items {
		row := make([]any, len(names))
		for i, n := range names {
			if v, ok := it[n]; ok {
				row[i] = cell(v)
			}
		}
		res.Rows = append(res.Rows, row)
		res.Docs = append(res.Docs, itemJSON(it))
	}
	return res
}

// ---- Apply ----------------------------------------------------------------------------

type step struct {
	admin *adminStmt
	shown string
	ps    []types.ParameterizedStatement
	texts []string
}

func paramsOf(args []any) ([]types.AttributeValue, error) {
	out := make([]types.AttributeValue, len(args))
	for i, a := range args {
		av, ok := a.(types.AttributeValue)
		if !ok {
			return nil, fmt.Errorf("parameter %d is %T, not a DynamoDB attribute", i+1, a)
		}
		out[i] = av
	}
	return out, nil
}

// Apply runs the statements in order. Consecutive PartiQL statements form one ExecuteTransaction
// (atomic; refused up front beyond 100 statements, so nothing is half applied for that reason). The
// table-level statements are separate API calls and are not atomic.
func (s *session) Apply(ctx context.Context, stmts []db.Statement) error {
	var steps []step
	for _, st := range stmts {
		text := stripLead(st.Text)
		a, err := parseAdmin(text)
		if err != nil {
			return fmt.Errorf("%s: %w", st.Shown(), err)
		}
		if a != nil {
			steps = append(steps, step{admin: a, shown: st.Shown()})
			continue
		}
		params, err := paramsOf(st.Args)
		if err != nil {
			return fmt.Errorf("%s: %w", st.Shown(), err)
		}
		if n := len(steps); n == 0 || steps[n-1].admin != nil {
			steps = append(steps, step{})
		}
		g := &steps[len(steps)-1]
		g.ps = append(g.ps, types.ParameterizedStatement{Statement: aws.String(text), Parameters: params})
		g.texts = append(g.texts, st.Shown())
	}
	for _, g := range steps {
		if len(g.ps) > maxTransaction {
			return fmt.Errorf("%d statements in one commit exceed DynamoDB's limit of %d per transaction; commit fewer rows at a time (nothing was applied)", len(g.ps), maxTransaction)
		}
	}
	defer s.forget() // columns and key types may have changed
	for i, g := range steps {
		if g.admin != nil {
			if _, err := s.runAdmin(ctx, g.admin, i < len(steps)-1); err != nil {
				return fmt.Errorf("%s: %w", g.shown, err)
			}
			continue
		}
		if _, err := s.client().ExecuteTransaction(ctx, &dynamodb.ExecuteTransactionInput{TransactStatements: g.ps}); err != nil {
			var tc *types.TransactionCanceledException
			if errors.As(err, &tc) {
				for j, r := range tc.CancellationReasons {
					if c := aws.ToString(r.Code); c != "None" && j < len(g.texts) {
						return fmt.Errorf("%s: %s: %s (transaction cancelled, nothing was applied)", g.texts[j], c, aws.ToString(r.Message))
					}
				}
			}
			return wrap("transaction", err)
		}
	}
	return nil
}

// ---- table-level operations -------------------------------------------------------------

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func backoff(attempt int) time.Duration { return min(50*time.Millisecond<<attempt, 5*time.Second) }

// waitActive polls until the table and all its indexes are ACTIVE (a deleted index drops out of the list).
func (s *session) waitActive(ctx context.Context, table string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for i := 0; ; i++ {
		d, err := s.describe(ctx, table)
		if err != nil {
			return err
		}
		ready := d.TableStatus == types.TableStatusActive
		for _, g := range d.GlobalSecondaryIndexes {
			ready = ready && g.IndexStatus == types.IndexStatusActive
		}
		if ready {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("table %s is still %s after %s; it keeps going in the background", table, d.TableStatus, timeout)
		}
		if err := sleepCtx(ctx, min(200*time.Millisecond<<min(i, 4), 3*time.Second)); err != nil {
			return err
		}
	}
}

func (s *session) waitGone(ctx context.Context, table string) {
	for i := 0; i < 60; i++ {
		_, err := s.client().DescribeTable(ctx, &dynamodb.DescribeTableInput{TableName: aws.String(table)})
		var nf *types.ResourceNotFoundException
		if errors.As(err, &nf) || ctx.Err() != nil {
			return
		}
		if sleepCtx(ctx, time.Second) != nil {
			return
		}
	}
}

// runAdmin executes a table-level statement; more says other statements follow, so an index change is
// waited on until finished (DynamoDB allows only one at a time).
func (s *session) runAdmin(ctx context.Context, a *adminStmt, more bool) (string, error) {
	cl := s.client()
	defer s.forget()
	switch a.op {
	case "create", "like":
		sp := a.create
		if a.op == "like" {
			d, err := s.describe(ctx, a.other)
			if err != nil {
				return "", err
			}
			sp = specOf(d)
		}
		in, err := createInput(a.table, sp)
		if err != nil {
			return "", err
		}
		if _, err := cl.CreateTable(ctx, in); err != nil {
			return "", wrap("create table "+a.table, err)
		}
		if err := s.waitActive(ctx, a.table, 5*time.Minute); err != nil {
			return "", err
		}
		msg := "table " + a.table + " created"
		if a.op == "like" && a.withData {
			n, err := s.copyItems(ctx, a.other, a.table)
			if err != nil {
				return "", fmt.Errorf("table %s was created but copying stopped after %d items: %w", a.table, n, err)
			}
			msg += fmt.Sprintf(", %d items copied", n)
		} else if a.op == "like" {
			msg += " (structure only; TTL, streams and tags are not copied)"
		}
		return msg, nil
	case "drop":
		if _, err := cl.DeleteTable(ctx, &dynamodb.DeleteTableInput{TableName: aws.String(a.table)}); err != nil {
			return "", wrap("drop table "+a.table, err)
		}
		s.waitGone(ctx, a.table)
		return "table " + a.table + " dropped", nil
	case "truncate":
		n, err := s.truncate(ctx, a.table)
		if err != nil {
			return "", fmt.Errorf("truncate stopped after %d items: %w", n, err)
		}
		return fmt.Sprintf("%d items deleted from %s", n, a.table), nil
	case "add_index":
		return s.addIndex(ctx, a, more)
	case "drop_index":
		if _, err := cl.UpdateTable(ctx, &dynamodb.UpdateTableInput{TableName: aws.String(a.table), GlobalSecondaryIndexUpdates: []types.GlobalSecondaryIndexUpdate{
			{Delete: &types.DeleteGlobalSecondaryIndexAction{IndexName: aws.String(a.other)}}}}); err != nil {
			return "", wrap("drop index "+a.other, err)
		}
		return s.afterIndexChange(ctx, a.table, "index "+a.other+" dropped", more)
	}
	return "", fmt.Errorf("unknown operation %q", a.op)
}

func (s *session) afterIndexChange(ctx context.Context, table, msg string, more bool) (string, error) {
	timeout := 10 * time.Second // the last change keeps building in the background
	if more {
		timeout = 30 * time.Minute
	}
	if err := s.waitActive(ctx, table, timeout); err != nil {
		if more || ctx.Err() != nil {
			return "", err
		}
		return msg + " (still being built in the background)", nil
	}
	return msg, nil
}

func (s *session) addIndex(ctx context.Context, a *adminStmt, more bool) (string, error) {
	is := a.index
	if strings.EqualFold(is.Kind, "LSI") {
		return "", errors.New("local indexes can only be created together with the table")
	}
	d, err := s.describe(ctx, a.table)
	if err != nil {
		return "", err
	}
	typ, order := map[string]string{}, []string{}
	for _, ad := range d.AttributeDefinitions {
		typ[aws.ToString(ad.AttributeName)] = string(ad.AttributeType)
		order = append(order, aws.ToString(ad.AttributeName))
	}
	if err := attrDefs(typ, &order, is.Keys); err != nil {
		return "", err
	}
	ks, err := keySchemaOf(is.Keys, "index "+is.Name)
	if err != nil {
		return "", err
	}
	proj, err := projectionOf(is)
	if err != nil {
		return "", err
	}
	g := &types.CreateGlobalSecondaryIndexAction{IndexName: aws.String(is.Name), KeySchema: ks, Projection: proj}
	if d.BillingModeSummary == nil || d.BillingModeSummary.BillingMode == types.BillingModeProvisioned {
		r, w := is.RCU, is.WCU
		if r == 0 && w == 0 && d.ProvisionedThroughput != nil {
			r, w = aws.ToInt64(d.ProvisionedThroughput.ReadCapacityUnits), aws.ToInt64(d.ProvisionedThroughput.WriteCapacityUnits)
		}
		if g.ProvisionedThroughput, err = throughput(r, w, "index "+is.Name); err != nil {
			return "", err
		}
	}
	if _, err := s.client().UpdateTable(ctx, &dynamodb.UpdateTableInput{TableName: aws.String(a.table), AttributeDefinitions: defsList(typ, order),
		GlobalSecondaryIndexUpdates: []types.GlobalSecondaryIndexUpdate{{Create: g}}}); err != nil {
		return "", wrap("add index "+is.Name, err)
	}
	return s.afterIndexChange(ctx, a.table, "index "+is.Name+" created", more)
}

// batchWrite sends writes in batches of 25, retrying what DynamoDB leaves unprocessed (throttling) with backoff.
func (s *session) batchWrite(ctx context.Context, table string, reqs []types.WriteRequest) error {
	for len(reqs) > 0 {
		pending := reqs[:min(25, len(reqs))]
		reqs = reqs[len(pending):]
		for attempt := 0; len(pending) > 0; attempt++ {
			out, err := s.client().BatchWriteItem(ctx, &dynamodb.BatchWriteItemInput{RequestItems: map[string][]types.WriteRequest{table: pending}})
			if err != nil {
				return wrap("batch write "+table, err)
			}
			if pending = out.UnprocessedItems[table]; len(pending) > 0 {
				if attempt >= 8 {
					return fmt.Errorf("batch write %s: still throttled after %d retries, %d items not written", table, attempt, len(pending))
				}
				if err := sleepCtx(ctx, backoff(attempt)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// truncate deletes every item: scan the keys, delete in batches.
func (s *session) truncate(ctx context.Context, table string) (int, error) {
	d, err := s.describe(ctx, table)
	if err != nil {
		return 0, err
	}
	e := newExpr()
	var ph []string
	for _, k := range d.KeySchema {
		ph = append(ph, e.name(aws.ToString(k.AttributeName)))
	}
	n := 0
	var start map[string]types.AttributeValue
	for {
		out, err := s.client().Scan(ctx, &dynamodb.ScanInput{TableName: aws.String(table), ProjectionExpression: aws.String(strings.Join(ph, ", ")),
			ExpressionAttributeNames: e.names, ExclusiveStartKey: start, ConsistentRead: aws.Bool(true)})
		if err != nil {
			return n, wrap("scan "+table, err)
		}
		reqs := make([]types.WriteRequest, len(out.Items))
		for i, it := range out.Items {
			reqs[i] = types.WriteRequest{DeleteRequest: &types.DeleteRequest{Key: it}}
		}
		if err := s.batchWrite(ctx, table, reqs); err != nil {
			return n, err
		}
		n += len(reqs)
		if start = out.LastEvaluatedKey; start == nil {
			return n, nil
		}
	}
}

// copyItems copies every item of src into dst (same key schema), page by page.
func (s *session) copyItems(ctx context.Context, src, dst string) (int, error) {
	n := 0
	var start map[string]types.AttributeValue
	for {
		out, err := s.client().Scan(ctx, &dynamodb.ScanInput{TableName: aws.String(src), ExclusiveStartKey: start, ConsistentRead: aws.Bool(true)})
		if err != nil {
			return n, wrap("scan "+src, err)
		}
		reqs := make([]types.WriteRequest, len(out.Items))
		for i, it := range out.Items {
			reqs[i] = types.WriteRequest{PutRequest: &types.PutRequest{Item: it}}
		}
		if err := s.batchWrite(ctx, dst, reqs); err != nil {
			return n, err
		}
		n += len(reqs)
		if start = out.LastEvaluatedKey; start == nil {
			return n, nil
		}
	}
}
