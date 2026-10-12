-- ======== 通知信(D88) ========

-- name: EnqueueNotifications :execrows
-- 對指定使用者(須啟用且有 email)排入寄信佇列;同一人同一 dedupe_key 只排一次
INSERT INTO notifications (company_id, user_id, to_email, event, subject, body, dedupe_key)
SELECT u.company_id, u.id, u.email, @event::varchar, @subject, @body, @dedupe_key
FROM users u
WHERE u.company_id = @company_id AND u.id = ANY(@user_ids::bigint[]) AND u.is_active
  AND COALESCE(u.email, '') <> ''
  AND NOT EXISTS (SELECT 1 FROM notification_prefs np WHERE np.user_id = u.id AND np.event = @event::varchar AND NOT np.enabled)
ON CONFLICT (user_id, dedupe_key) WHERE dedupe_key <> '' DO NOTHING;

-- name: ClaimNotifications :many
-- 取出待寄的信並預約 10 分鐘(寄送中程式中斷時會自動重試);attempts 在這裡 +1
UPDATE notifications SET attempts = attempts + 1, next_attempt_at = now() + interval '10 minutes'
WHERE id IN (
    SELECT n.id FROM notifications n
    WHERE n.status = 'pending' AND n.next_attempt_at <= now()
    ORDER BY n.id LIMIT @batch FOR UPDATE SKIP LOCKED
) RETURNING *;

-- name: MarkNotificationSent :exec
UPDATE notifications SET status = 'sent', sent_at = now(), last_error = '' WHERE id = @id;

-- name: MarkNotificationFailed :exec
UPDATE notifications SET last_error = @last_error,
    status = CASE WHEN attempts >= 5 THEN 'failed' ELSE 'pending' END,
    next_attempt_at = now() + (attempts * interval '5 minutes')
WHERE id = @id;

-- name: PurgeOldNotifications :execrows
DELETE FROM notifications WHERE status <> 'pending' AND created_at < now() - interval '60 days';

-- name: ApproverIDsByPermission :many
-- 具某核准權限的人(含超級管理員),加上此時有效的代理人
WITH base AS (
    SELECT u.id FROM users u WHERE u.company_id = @company_id AND u.is_active AND u.is_superadmin
    UNION
    SELECT ur.user_id FROM user_roles ur
    JOIN roles r ON r.id = ur.role_id AND r.is_active AND r.company_id = @company_id
    JOIN role_permissions rp ON rp.role_id = r.id AND rp.permission = @permission::text
)
SELECT id FROM base
UNION
SELECT d.delegate_id FROM approval_delegations d
WHERE d.company_id = @company_id AND d.delegator_id IN (SELECT id FROM base)
  AND CURRENT_DATE BETWEEN d.valid_from AND d.valid_to;

-- name: ApproverIDsByRole :many
-- 具某角色的人,加上此時有效的代理人
WITH base AS (
    SELECT ur.user_id AS id FROM user_roles ur
    JOIN roles r ON r.id = ur.role_id AND r.is_active AND r.company_id = @company_id
    WHERE ur.role_id = @role_id
)
SELECT id FROM base
UNION
SELECT d.delegate_id FROM approval_delegations d
WHERE d.company_id = @company_id AND d.delegator_id IN (SELECT id FROM base)
  AND CURRENT_DATE BETWEEN d.valid_from AND d.valid_to;

-- name: EnqueueCheckDueNotifications :execrows
-- 未兌現票據在 @days 天內到期(含已逾期):通知具該方向過帳權限的人(含超級管理員);每張票據每人只通知一次
INSERT INTO notifications (company_id, user_id, to_email, event, subject, body, dedupe_key)
SELECT k.company_id, u.id, u.email, 'check_due',
       CASE k.side WHEN 'receipt' THEN '應收票據' ELSE '應付票據' END || (CASE WHEN k.due_date < CURRENT_DATE THEN '已逾期:' ELSE '即將到期:' END) || k.check_no,
       CASE k.side WHEN 'receipt' THEN '應收票據' ELSE '應付票據' END || ' ' || k.check_no || ',金額 ' || k.amount::numeric(18,0)::text
         || ',到期日 ' || to_char(k.due_date, 'YYYY-MM-DD') || '。' || E'\n\n' || @base_url::text || '/finance/' || (CASE k.side WHEN 'receipt' THEN 'checks/collection' ELSE 'checks/payment' END),
       'check_due:' || k.id
FROM checks k
JOIN users u ON u.company_id = k.company_id AND u.is_active AND COALESCE(u.email, '') <> ''
WHERE k.status = 'pending' AND k.due_date <= CURRENT_DATE + @days::int
  AND NOT EXISTS (SELECT 1 FROM notification_prefs np WHERE np.user_id = u.id AND np.event = 'check_due' AND NOT np.enabled)
  AND (u.is_superadmin OR EXISTS (
        SELECT 1 FROM user_roles ur JOIN roles r ON r.id = ur.role_id AND r.is_active
        JOIN role_permissions rp ON rp.role_id = r.id
        WHERE ur.user_id = u.id
          AND rp.permission = CASE k.side WHEN 'receipt' THEN 'finance.collection.post' ELSE 'finance.payment.post' END))
ON CONFLICT (user_id, dedupe_key) WHERE dedupe_key <> '' DO NOTHING;

-- name: ListNotificationPrefs :many
SELECT event, enabled FROM notification_prefs WHERE user_id = @user_id;

-- name: UpsertNotificationPref :exec
INSERT INTO notification_prefs (user_id, event, enabled) VALUES (@user_id, @event, @enabled)
ON CONFLICT (user_id, event) DO UPDATE SET enabled = EXCLUDED.enabled;

-- name: EnqueueLowStockDigest :execrows
-- 每週一封摘要:低於安全庫存(所有倉庫合計)的料品數與缺口最大的 10 項;給具庫存檢視權限的人與超級管理員
WITH low AS (
    SELECT i.company_id, i.code, i.name, t.total, i.safety_stock,
           row_number() OVER (PARTITION BY i.company_id ORDER BY (i.safety_stock - t.total) DESC, i.code) AS rn
    FROM items i
    JOIN LATERAL (SELECT COALESCE(SUM(b.qty), 0)::numeric AS total FROM inventory_balances b WHERE b.item_id = i.id) t ON TRUE
    WHERE i.is_active AND i.item_type = 'goods' AND i.safety_stock > 0 AND t.total < i.safety_stock
), agg AS (
    SELECT company_id, count(*) AS n,
           string_agg(code || ' ' || name || ':現有 ' || trim_scale(total)::text || ' / 安全庫存 ' || trim_scale(safety_stock)::text, E'\n' ORDER BY rn) FILTER (WHERE rn <= 10) AS lines
    FROM low GROUP BY company_id
)
INSERT INTO notifications (company_id, user_id, to_email, event, subject, body, dedupe_key)
SELECT a.company_id, u.id, u.email, 'low_stock', '低於安全庫存:' || a.n || ' 項料品',
       a.lines || CASE WHEN a.n > 10 THEN E'\n…等共 ' || a.n || ' 項' ELSE '' END || E'\n\n' || @base_url::text || '/inventory/stock',
       'low_stock:' || to_char(CURRENT_DATE, 'IYYY-"W"IW')
FROM agg a
JOIN users u ON u.company_id = a.company_id AND u.is_active AND COALESCE(u.email, '') <> ''
WHERE (u.is_superadmin OR EXISTS (
        SELECT 1 FROM user_roles ur JOIN roles r ON r.id = ur.role_id AND r.is_active
        JOIN role_permissions rp ON rp.role_id = r.id
        WHERE ur.user_id = u.id AND rp.permission = 'inventory.stock.read'))
  AND NOT EXISTS (SELECT 1 FROM notification_prefs np WHERE np.user_id = u.id AND np.event = 'low_stock' AND NOT np.enabled)
ON CONFLICT (user_id, dedupe_key) WHERE dedupe_key <> '' DO NOTHING;

-- name: EnqueueLotExpiryDigest :execrows
-- 每週一封摘要:有庫存的批號已過期或 @days 天內到期;給具庫存檢視權限的人與超級管理員
WITH lots AS (
    SELECT b.company_id, l.lot_no, l.expiry_date, i.code, i.name, SUM(b.qty)::numeric AS qty,
           row_number() OVER (PARTITION BY b.company_id ORDER BY l.expiry_date, i.code, l.lot_no) AS rn
    FROM inventory_lot_balances b
    JOIN item_lots l ON l.id = b.lot_id
    JOIN items i ON i.id = b.item_id
    WHERE b.qty > 0 AND l.expiry_date IS NOT NULL AND l.expiry_date <= CURRENT_DATE + @days::int
    GROUP BY b.company_id, l.id, i.code, i.name
), agg AS (
    SELECT company_id, count(*) AS n,
           string_agg(code || ' ' || name || ' 批號 ' || lot_no || ',數量 ' || trim_scale(qty)::text || ',效期 ' || to_char(expiry_date, 'YYYY-MM-DD')
                      || CASE WHEN expiry_date < CURRENT_DATE THEN '(已過期)' ELSE '' END, E'\n' ORDER BY rn) FILTER (WHERE rn <= 10) AS lines
    FROM lots GROUP BY company_id
)
INSERT INTO notifications (company_id, user_id, to_email, event, subject, body, dedupe_key)
SELECT a.company_id, u.id, u.email, 'lot_expiry', '批號效期預警:' || a.n || ' 個批號',
       a.lines || CASE WHEN a.n > 10 THEN E'\n…等共 ' || a.n || ' 個批號' ELSE '' END || E'\n\n' || @base_url::text || '/inventory/lots',
       'lot_expiry:' || to_char(CURRENT_DATE, 'IYYY-"W"IW')
FROM agg a
JOIN users u ON u.company_id = a.company_id AND u.is_active AND COALESCE(u.email, '') <> ''
WHERE (u.is_superadmin OR EXISTS (
        SELECT 1 FROM user_roles ur JOIN roles r ON r.id = ur.role_id AND r.is_active
        JOIN role_permissions rp ON rp.role_id = r.id
        WHERE ur.user_id = u.id AND rp.permission = 'inventory.stock.read'))
  AND NOT EXISTS (SELECT 1 FROM notification_prefs np WHERE np.user_id = u.id AND np.event = 'lot_expiry' AND NOT np.enabled)
ON CONFLICT (user_id, dedupe_key) WHERE dedupe_key <> '' DO NOTHING;

-- name: EnqueueArOverdueDigest :execrows
-- 每週一封摘要:逾期未收的應收筆數與本位幣金額;只給具收款過帳權限且資料範圍為「全部」的人與超級管理員(範圍受限者看不到全公司金額)
WITH od AS (
    SELECT r.company_id, count(*) AS n, SUM(b.bal)::numeric AS amount
    FROM accounts_receivable r
    CROSS JOIN LATERAL (SELECT (CASE WHEN r.amount = 0 THEN 0 ELSE r.base_amount * (r.amount - r.paid_amount) / r.amount END)::numeric AS bal) b
    WHERE r.amount <> r.paid_amount AND r.due_date < CURRENT_DATE
    GROUP BY r.company_id
)
INSERT INTO notifications (company_id, user_id, to_email, event, subject, body, dedupe_key)
SELECT o.company_id, u.id, u.email, 'ar_overdue', '逾期應收帳款:' || o.n || ' 筆',
       '逾期未收 ' || o.n || ' 筆,未沖金額(本位幣)' || o.amount::numeric(18,0)::text || E'。\n\n' || @base_url::text || '/finance/receivables',
       'ar_overdue:' || to_char(CURRENT_DATE, 'IYYY-"W"IW')
FROM od o
JOIN users u ON u.company_id = o.company_id AND u.is_active AND COALESCE(u.email, '') <> ''
WHERE o.amount > 0
  AND (u.is_superadmin OR EXISTS (
        SELECT 1 FROM user_roles ur JOIN roles r ON r.id = ur.role_id AND r.is_active AND r.data_scope = 'all'
        JOIN role_permissions rp ON rp.role_id = r.id
        WHERE ur.user_id = u.id AND rp.permission = 'finance.collection.post'))
  AND NOT EXISTS (SELECT 1 FROM notification_prefs np WHERE np.user_id = u.id AND np.event = 'ar_overdue' AND NOT np.enabled)
ON CONFLICT (user_id, dedupe_key) WHERE dedupe_key <> '' DO NOTHING;
