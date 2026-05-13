-- Phase2: command progress、配置下发审计字段

ALTER TABLE command_task ADD COLUMN IF NOT EXISTS log_summary TEXT;

ALTER TABLE config_deploy_task ADD COLUMN IF NOT EXISTS progress_log TEXT;
ALTER TABLE config_deploy_task ADD COLUMN IF NOT EXISTS target_config_version_id BIGINT REFERENCES config_version(id);
ALTER TABLE config_deploy_task ADD COLUMN IF NOT EXISTS before_config_hash CHAR(64);
ALTER TABLE config_deploy_task ADD COLUMN IF NOT EXISTS after_config_hash CHAR(64);
ALTER TABLE config_deploy_task ADD COLUMN IF NOT EXISTS related_command_id UUID;

UPDATE config_deploy_task SET target_config_version_id = config_version_id WHERE target_config_version_id IS NULL AND config_version_id IS NOT NULL;
UPDATE config_deploy_task SET before_config_hash = old_config_hash WHERE before_config_hash IS NULL AND old_config_hash IS NOT NULL;
UPDATE config_deploy_task SET after_config_hash = new_config_hash WHERE after_config_hash IS NULL AND new_config_hash IS NOT NULL;
