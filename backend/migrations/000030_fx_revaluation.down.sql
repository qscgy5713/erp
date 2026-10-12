DROP TABLE fx_revaluations;
DELETE FROM account_mappings WHERE map_key IN ('fx.unrealized_gain', 'fx.unrealized_loss');
