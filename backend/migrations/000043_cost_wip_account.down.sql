DELETE FROM account_mappings WHERE map_key = 'cost.wip';
ALTER TABLE cost_closings DROP COLUMN wip_value;
