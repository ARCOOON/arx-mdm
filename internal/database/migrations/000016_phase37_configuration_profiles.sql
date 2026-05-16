-- Phase 37: Deep device configuration profiles, principal/device groups, and app managed configuration.

CREATE TABLE principal_groups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE principal_groups IS 'Logical cohorts of managed devices used for scoped profile assignment.';

CREATE UNIQUE INDEX principal_groups_name_lower_idx ON principal_groups (lower(trim(name)));

CREATE TABLE principal_group_devices (
    group_id UUID NOT NULL REFERENCES principal_groups (id) ON DELETE CASCADE,
    device_id UUID NOT NULL REFERENCES assets (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, device_id)
);

CREATE INDEX principal_group_devices_device_idx ON principal_group_devices (device_id);

CREATE TABLE configuration_profiles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    platform TEXT NOT NULL CHECK (platform IN ('windows', 'linux', 'android')),
    type TEXT NOT NULL CHECK (length(trim(type)) > 0),
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX configuration_profiles_platform_idx ON configuration_profiles (platform);

CREATE TABLE profile_assignments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    profile_id UUID NOT NULL REFERENCES configuration_profiles (id) ON DELETE CASCADE,
    target_kind TEXT NOT NULL CHECK (
        target_kind IN ('device', 'principal_group')
    ),
    device_id UUID REFERENCES assets (id) ON DELETE CASCADE,
    principal_group_id UUID REFERENCES principal_groups (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT profile_assignments_target_exclusive CHECK (
        (
            target_kind = 'device'
            AND device_id IS NOT NULL
            AND principal_group_id IS NULL
        )
        OR (
            target_kind = 'principal_group'
            AND principal_group_id IS NOT NULL
            AND device_id IS NULL
        )
    )
);

CREATE UNIQUE INDEX profile_assignments_device_uq
    ON profile_assignments (profile_id, device_id)
    WHERE
        target_kind = 'device';

CREATE UNIQUE INDEX profile_assignments_group_uq
    ON profile_assignments (profile_id, principal_group_id)
    WHERE
        target_kind = 'principal_group';

CREATE INDEX profile_assignments_profile_idx ON profile_assignments (profile_id);

CREATE TABLE app_configurations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    catalog_app_id UUID NOT NULL REFERENCES app_catalog (id) ON DELETE CASCADE,
    managed_package_name TEXT NOT NULL CHECK (length(trim(managed_package_name)) > 0),
    managed_app_label TEXT NOT NULL DEFAULT '',
    config_kv JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON COLUMN app_configurations.managed_package_name IS 'Managed application package identifier (Android applicationId / installer subject).';

CREATE UNIQUE INDEX app_configurations_catalog_pkg_uq ON app_configurations (catalog_app_id, managed_package_name);
