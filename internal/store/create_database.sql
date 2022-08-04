/* creates a new, empty database */

CREATE EXTENSION semver;

CREATE TABLE module (
    id              SERIAL PRIMARY KEY,
    name            TEXT NOT NULL,
    description     TEXT 
);
ALTER TABLE module
    ADD CONSTRAINT uc_module_name
    UNIQUE (name);

CREATE TABLE module_version (
    id          SERIAL PRIMARY KEY,
    module_id   INTEGER REFERENCES module(id),
    version     SEMVER NOT NULL
);

ALTER TABLE module_version
    ADD CONSTRAINT uc_module_version_module_id_version
    UNIQUE (module_id, version);

CREATE TABLE module_dependency (
    dependent_id    INTEGER REFERENCES module_version(id),
    dependee_id     INTEGER REFERENCES module_version(id),
    PRIMARY KEY(dependent_id, dependee_id)
);
