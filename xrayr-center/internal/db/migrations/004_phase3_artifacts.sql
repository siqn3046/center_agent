-- Phase3：XrayR 二进制制品表、节点 allow_install 权限

ALTER TABLE node ADD COLUMN IF NOT EXISTS allow_install BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE IF NOT EXISTS xrayr_binary_artifact (
    id BIGSERIAL PRIMARY KEY,
    display_name VARCHAR(256) NOT NULL,
    target_os VARCHAR(32) NOT NULL,
    target_arch VARCHAR(32) NOT NULL,
    version_label VARCHAR(128),
    sha256_hex CHAR(64) NOT NULL,
    storage_relpath VARCHAR(512) NOT NULL,
    disabled BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_xrayr_artifact_os_arch ON xrayr_binary_artifact(target_os, target_arch) WHERE disabled = FALSE;
