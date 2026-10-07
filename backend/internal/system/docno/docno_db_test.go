package docno

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"erp/internal/db"
	"erp/internal/platform/database"
	"erp/internal/testutil/dbtest"
)

func TestNextSequentialAndPeriodReset(t *testing.T) {
	store := database.NewStore(dbtest.New(t))
	ctx := context.Background()
	d1 := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	d2 := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		date time.Time
		want string
	}{
		{d1, "PO202610070001"}, {d1, "PO202610070002"}, {d2, "PO202610080001"}, {d1, "PO202610070003"},
	} {
		got, err := Next(ctx, store.Queries, 1, "purchase_order", tc.date)
		if err != nil || got != tc.want {
			t.Fatalf("got %s err=%v, want %s", got, err, tc.want)
		}
	}
	if _, err := Next(ctx, store.Queries, 1, "no_such_type", d1); err == nil {
		t.Fatal("未定義的單據類型應回傳錯誤")
	}
}

func TestNextRollbackDoesNotSkip(t *testing.T) {
	store := database.NewStore(dbtest.New(t))
	ctx := context.Background()
	d := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	_ = store.InTx(ctx, func(q *db.Queries) error {
		if _, err := Next(ctx, q, 1, "sales_order", d); err != nil {
			t.Fatal(err)
		}
		return pgx.ErrTxClosed // 讓交易回滾
	})
	got, err := Next(ctx, store.Queries, 1, "sales_order", d)
	if err != nil || got != "SO202601010001" {
		t.Fatalf("回滾後應沿用同一號,got %s err=%v", got, err)
	}
}

func TestNextConcurrentUnique(t *testing.T) {
	store := database.NewStore(dbtest.New(t))
	ctx := context.Background()
	d := time.Date(2026, 3, 3, 0, 0, 0, 0, time.UTC)

	const n = 30
	var (
		mu   sync.Mutex
		seen = map[string]bool{}
		wg   sync.WaitGroup
	)
	for range n {
		wg.Go(func() {
			err := store.InTx(ctx, func(q *db.Queries) error {
				no, err := Next(ctx, q, 1, "delivery", d)
				if err != nil {
					return err
				}
				mu.Lock()
				defer mu.Unlock()
				if seen[no] {
					t.Errorf("重複單號 %s", no)
				}
				seen[no] = true
				return nil
			})
			if err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if len(seen) != n || !seen["DN202603030030"] {
		t.Fatalf("應產生 %d 個連續單號,got %d", n, len(seen))
	}
}
