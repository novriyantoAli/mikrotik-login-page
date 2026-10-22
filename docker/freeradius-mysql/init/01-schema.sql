-- Minimal FreeRADIUS schema for the hotspot (ADR 0001/0002).
-- The backend writes these tables directly; FreeRADIUS reads them at login.
-- Loaded on first boot by the mysql image's docker-entrypoint-initdb.d hook.

CREATE TABLE IF NOT EXISTS radcheck (
    id        INT UNSIGNED NOT NULL AUTO_INCREMENT,
    username  VARCHAR(64)  NOT NULL DEFAULT '',
    attribute VARCHAR(64)  NOT NULL DEFAULT '',
    op        VARCHAR(2)   NOT NULL DEFAULT '==',
    value     VARCHAR(253) NOT NULL DEFAULT '',
    PRIMARY KEY (id),
    KEY username (username(32))
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4;

CREATE TABLE IF NOT EXISTS radreply (
    id        INT UNSIGNED NOT NULL AUTO_INCREMENT,
    username  VARCHAR(64)  NOT NULL DEFAULT '',
    attribute VARCHAR(64)  NOT NULL DEFAULT '',
    op        VARCHAR(2)   NOT NULL DEFAULT '=',
    value     VARCHAR(253) NOT NULL DEFAULT '',
    PRIMARY KEY (id),
    KEY username (username(32))
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4;
