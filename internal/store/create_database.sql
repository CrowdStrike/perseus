/* creates a new, empty database */

CREATE EXTENSION semver;

CREATE TABLE module (
    id              SERIAL,
    name            TEXT NOT NULL,
    description     TEXT,
    created_at      TIMESTAMP NOT NULL DEFAULT (TIMESTAMP('utc', now())),
    CONSTRAINT pk_module
        PRIMARY KEY(id),
    CONSTRAINT uc_module_name
        UNIQUE(name)
);

CREATE INDEX ix_module_created_at
    ON module USING btree
    (created_at DESC);

CREATE TABLE module_version (
    id          SERIAL,
    module_id   INTEGER NOT NULL,
    version     SEMVER NOT NULL,
    created_at  TIMESTAMP NOT NULL DEFAULT (TIMESTAMP('utc', now())),
    CONSTRAINT pk_module_version
        PRIMARY KEY(id),
    CONSTRAINT uc_module_version_module_id_version
        UNIQUE (module_id, version),
    CONSTRAINT fk_module_version_module_id_module_id
        FOREIGN KEY(module_id) REFERENCES module (id)
        ON UPDATE NO ACTION
        ON DELETE CASCADE
);

CREATE INDEX ix_module_version_created_at
    ON module_module USING btree
    (created_at DESC);

CREATE INDEX idx_module_version_version
    ON module_version USING btree
    (module_id ASC NULLS LAST, version DESC NULLS FIRST);

CREATE TABLE module_dependency (
    dependent_id    INTEGER NOT NULL,
    dependee_id     INTEGER NOT NULL,
    CONSTRAINT pk_module_dependency
        PRIMARY KEY(dependent_id, dependee_id),
    CONSTRAINT fk_module_dependency_dependent_id_module_verison_id
        FOREIGN KEY(dependent_id) REFERENCES module_version (id)
        ON UPDATE NO ACTION
        ON DELETE CASCADE,
    CONSTRAINT fk_module_dependency_dependee_id_module_verison_id
        FOREIGN KEY(dependee_id) REFERENCES module_version (id)
        ON UPDATE NO ACTION
        ON DELETE CASCADE
);
