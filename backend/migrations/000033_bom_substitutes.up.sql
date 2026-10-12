-- BOM 替代料(D90):某個材料缺料時可改用的料品;ratio = 1 單位主料可由幾單位替代料取代
CREATE TABLE bom_line_substitutes (
    bom_id  BIGINT        NOT NULL,
    line_no INTEGER       NOT NULL,
    item_id BIGINT        NOT NULL REFERENCES items (id),
    ratio   NUMERIC(18,4) NOT NULL DEFAULT 1,
    PRIMARY KEY (bom_id, line_no, item_id),
    FOREIGN KEY (bom_id, line_no) REFERENCES bom_lines (bom_id, line_no) ON DELETE CASCADE,
    CONSTRAINT bom_line_substitutes_ratio_check CHECK (ratio > 0)
);
CREATE INDEX bom_line_substitutes_item_idx ON bom_line_substitutes (item_id);
