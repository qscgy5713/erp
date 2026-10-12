-- 通知訂閱(D93):每人每種事件可關閉;沒有紀錄 = 預設開啟
CREATE TABLE notification_prefs (
    user_id BIGINT      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    event   VARCHAR(30) NOT NULL,
    enabled BOOLEAN     NOT NULL,
    PRIMARY KEY (user_id, event)
);
