-- XrayR Center Phase 1 MVP schema (PostgreSQL)

CREATE TABLE IF NOT EXISTS admin_user (
    id BIGSERIAL PRIMARY KEY,
    username VARCHAR(64) NOT NULL UNIQUE,
    password_hash VARCHAR(255) NOT NULL,
    role VARCHAR(32) NOT NULL DEFAULT 'admin',
    disabled BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS node_group (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(128) NOT NULL UNIQUE,
    description TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS node (
    id BIGSERIAL PRIMARY KEY,
    node_code VARCHAR(64) NOT NULL UNIQUE,
    node_name VARCHAR(128) NOT NULL,
    node_group_id BIGINT REFERENCES node_group(id),
    public_ip VARCHAR(64),
    hostname VARCHAR(255),
    agent_version VARCHAR(32),
    install_state VARCHAR(32),
    manage_status VARCHAR(32) NOT NULL DEFAULT 'DISCOVERED',
    import_mode VARCHAR(32) NOT NULL DEFAULT 'AUTO_DETECT',
    discovered_binary_path TEXT,
    discovered_config_path TEXT,
    discovered_service_name VARCHAR(64),
    discovered_xrayr_version VARCHAR(64),
    discovered_xray_core_version VARCHAR(64),
    discovered_at TIMESTAMPTZ,
    last_discovery_error TEXT,
    imported_config_hash CHAR(64),
    imported_backup_path TEXT,
    allow_config_apply BOOLEAN NOT NULL DEFAULT FALSE,
    allow_restart BOOLEAN NOT NULL DEFAULT FALSE,
    allow_cleanup BOOLEAN NOT NULL DEFAULT FALSE,
    allow_upgrade BOOLEAN NOT NULL DEFAULT FALSE,
    node_hmac_key VARCHAR(128),
    last_seen_at TIMESTAMPTZ,
    disabled BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS node_install_token (
    id BIGSERIAL PRIMARY KEY,
    node_id BIGINT NOT NULL REFERENCES node(id) ON DELETE CASCADE,
    token_hash CHAR(64) NOT NULL UNIQUE,
    import_mode VARCHAR(32) NOT NULL DEFAULT 'AUTO_DETECT',
    install_xrayr_if_missing BOOLEAN NOT NULL DEFAULT TRUE,
    upgrade_xrayr_if_exists BOOLEAN NOT NULL DEFAULT FALSE,
    repair_if_broken BOOLEAN NOT NULL DEFAULT FALSE,
    target_xrayr_version VARCHAR(64),
    target_download_url TEXT,
    target_sha256 CHAR(64),
    auto_start_after_install BOOLEAN NOT NULL DEFAULT TRUE,
    auto_enable_manage_after_success BOOLEAN NOT NULL DEFAULT FALSE,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    created_by BIGINT REFERENCES admin_user(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_install_token_node ON node_install_token(node_id);

CREATE TABLE IF NOT EXISTS node_heartbeat (
    id BIGSERIAL PRIMARY KEY,
    node_id BIGINT NOT NULL REFERENCES node(id) ON DELETE CASCADE,
    uptime_sec BIGINT,
    agent_version VARCHAR(32),
    xrayr_running BOOLEAN,
    raw_json JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS node_monitor_snapshot (
    id BIGSERIAL PRIMARY KEY,
    node_id BIGINT NOT NULL REFERENCES node(id) ON DELETE CASCADE,
    cpu_pct DOUBLE PRECISION,
    mem_pct DOUBLE PRECISION,
    disk_pct DOUBLE PRECISION,
    net_in_bps DOUBLE PRECISION,
    net_out_bps DOUBLE PRECISION,
    load1 DOUBLE PRECISION,
    uptime_sec BIGINT,
    payload_json JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS node_network_snapshot (
    id BIGSERIAL PRIMARY KEY,
    monitor_id BIGINT REFERENCES node_monitor_snapshot(id) ON DELETE CASCADE,
    iface VARCHAR(32) NOT NULL,
    bytes_in BIGINT,
    bytes_out BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS xrayr_status_snapshot (
    id BIGSERIAL PRIMARY KEY,
    node_id BIGINT NOT NULL REFERENCES node(id) ON DELETE CASCADE,
    systemd_active BOOLEAN,
    config_hash CHAR(64),
    xrayr_version VARCHAR(64),
    last_restart_at TIMESTAMPTZ,
    error_tail TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS discovery_report (
    id BIGSERIAL PRIMARY KEY,
    node_id BIGINT NOT NULL REFERENCES node(id) ON DELETE CASCADE,
    install_state VARCHAR(32) NOT NULL,
    binary_paths JSONB,
    config_paths JSONB,
    service_status JSONB,
    process_status JSONB,
    version_info JSONB,
    config_hash CHAR(64),
    error_tail TEXT,
    raw_json JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_discovery_node_time ON discovery_report(node_id, created_at DESC);

CREATE TABLE IF NOT EXISTS config_template (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(128) NOT NULL UNIQUE,
    description TEXT,
    created_by BIGINT REFERENCES admin_user(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS config_version (
    id BIGSERIAL PRIMARY KEY,
    template_id BIGINT REFERENCES config_template(id) ON DELETE CASCADE,
    version VARCHAR(32) NOT NULL,
    content_yaml TEXT NOT NULL,
    content_sha256 CHAR(64) NOT NULL,
    source_type VARCHAR(32) NOT NULL DEFAULT 'TEMPLATE',
    imported_from_node_id BIGINT REFERENCES node(id),
    changelog TEXT,
    created_by BIGINT REFERENCES admin_user(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(template_id, version)
);

CREATE TABLE IF NOT EXISTS config_deploy_task (
    id BIGSERIAL PRIMARY KEY,
    node_id BIGINT NOT NULL REFERENCES node(id) ON DELETE CASCADE,
    config_version_id BIGINT REFERENCES config_version(id),
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    old_config_hash CHAR(64),
    new_config_hash CHAR(64) NOT NULL,
    backup_path TEXT,
    require_manual_confirm BOOLEAN NOT NULL DEFAULT FALSE,
    confirmed_by BIGINT REFERENCES admin_user(id),
    confirmed_at TIMESTAMPTZ,
    error_message TEXT,
    idempotency_key VARCHAR(128),
    created_by BIGINT REFERENCES admin_user(id),
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS command_task (
    id BIGSERIAL PRIMARY KEY,
    command_id UUID NOT NULL UNIQUE,
    node_id BIGINT NOT NULL REFERENCES node(id) ON DELETE CASCADE,
    command_type VARCHAR(64) NOT NULL,
    payload_json JSONB NOT NULL DEFAULT '{}',
    idempotency_key VARCHAR(128) NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    result_json JSONB,
    error_message TEXT,
    created_by BIGINT REFERENCES admin_user(id),
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(node_id, idempotency_key)
);

CREATE TABLE IF NOT EXISTS cleanup_task (
    id BIGSERIAL PRIMARY KEY,
    node_id BIGINT NOT NULL REFERENCES node(id) ON DELETE CASCADE,
    cleanup_type VARCHAR(32) NOT NULL,
    params_json JSONB NOT NULL,
    status VARCHAR(32) NOT NULL,
    result_json JSONB,
    error_message TEXT,
    created_by BIGINT REFERENCES admin_user(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS operation_log (
    id BIGSERIAL PRIMARY KEY,
    actor_admin_id BIGINT REFERENCES admin_user(id),
    action VARCHAR(128) NOT NULL,
    target_type VARCHAR(32),
    target_id VARCHAR(64),
    detail_json JSONB,
    ip VARCHAR(64),
    user_agent TEXT,
    success BOOLEAN NOT NULL,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS alert_event (
    id BIGSERIAL PRIMARY KEY,
    node_id BIGINT REFERENCES node(id) ON DELETE SET NULL,
    rule_code VARCHAR(64) NOT NULL,
    severity VARCHAR(16) NOT NULL,
    message TEXT NOT NULL,
    payload_json JSONB,
    dedupe_key VARCHAR(128),
    opened_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
