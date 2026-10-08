DROP TABLE IF EXISTS item_costs;
DROP TABLE IF EXISTS cost_closings;
DELETE FROM account_mappings WHERE map_key IN ('cost.cogs', 'cost.inventory', 'cost.adjustment');
DELETE FROM accounts WHERE code = '5102';
