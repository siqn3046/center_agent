-- Phase4：制品大小字段（可选），便于前端展示与校验

ALTER TABLE xrayr_binary_artifact ADD COLUMN IF NOT EXISTS size_bytes BIGINT;
