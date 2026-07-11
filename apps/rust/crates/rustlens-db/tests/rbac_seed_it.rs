//! Integration test for `rbac::seed_permissions_and_roles` against a real
//! Postgres. Skipped unless `RUSTLENS_TEST_PG_URL` is set (no-op in normal
//! `cargo test` / CI runs with no database).

use rustlens_db::rbac::{self, ALL_PERMISSIONS, SYSTEM_ROLES};

async fn clean(pool: &sqlx::PgPool) {
    for t in ["admin_role_permissions", "admin_user_roles", "admin_roles", "admin_permissions"] {
        sqlx::query(&format!("DELETE FROM {t}")).execute(pool).await.unwrap();
    }
}

#[tokio::test]
async fn seed_is_idempotent_and_fully_populates_the_catalog() {
    let Ok(url) = std::env::var("RUSTLENS_TEST_PG_URL") else {
        eprintln!("RUSTLENS_TEST_PG_URL unset, skipping RBAC seed integration test");
        return;
    };
    let pool = rustlens_db::connect(&url, 5).await.expect("connect");
    // The distilled 0001 baseline is the tested schema contract.
    rustlens_db::migrate(&pool).await.expect("migrate");
    clean(&pool).await;

    // First boot: full catalog populated.
    rbac::seed_permissions_and_roles(&pool).await.expect("first seed");

    let perm_count: i64 = sqlx::query_scalar("SELECT COUNT(*) FROM admin_permissions")
        .fetch_one(&pool)
        .await
        .unwrap();
    assert_eq!(perm_count, ALL_PERMISSIONS.len() as i64);

    let role_count: i64 = sqlx::query_scalar("SELECT COUNT(*) FROM admin_roles")
        .fetch_one(&pool)
        .await
        .unwrap();
    assert_eq!(role_count, SYSTEM_ROLES.len() as i64);

    // Every permission key from the static catalog exists in the DB.
    for p in ALL_PERMISSIONS {
        let desc: String = sqlx::query_scalar("SELECT description FROM admin_permissions WHERE key = $1")
            .bind(p.key)
            .fetch_one(&pool)
            .await
            .unwrap();
        assert_eq!(desc, p.description);
    }

    // Every system role exists with the right is_system flag and permission set.
    for r in SYSTEM_ROLES {
        let row: (String, String, String, bool) = sqlx::query_as(
            "SELECT id::text, name, COALESCE(description, ''), is_system FROM admin_roles WHERE key = $1",
        )
        .bind(r.key)
        .fetch_one(&pool)
        .await
        .unwrap();
        assert_eq!(row.1, r.name);
        assert_eq!(row.2, r.description);
        assert_eq!(row.3, r.is_system);

        let mut perms: Vec<String> =
            sqlx::query_scalar("SELECT permission_key FROM admin_role_permissions WHERE role_id::text = $1")
                .bind(&row.0)
                .fetch_all(&pool)
                .await
                .unwrap();
        perms.sort();
        let mut want: Vec<String> = r.permissions.iter().map(|s| s.to_string()).collect();
        want.sort();
        assert_eq!(perms, want, "role {} permission set mismatch", r.key);
    }

    // Capture role IDs + timestamps + a hand-edit to a system role's
    // permissions, to prove re-seeding is a no-op except where Go's seed
    // itself overwrites (name/description/is_system/updated_at, and the
    // permission set of every non-superadmin role is always reset).
    let superadmin_id: String =
        sqlx::query_scalar("SELECT id::text FROM admin_roles WHERE key = 'superadmin'").fetch_one(&pool).await.unwrap();
    let moderator_id: String =
        sqlx::query_scalar("SELECT id::text FROM admin_roles WHERE key = 'moderator'").fetch_one(&pool).await.unwrap();

    // Hand-edit moderator's permission set; the next seed must reset it back
    // to the static catalog (mirrors Go's unconditional DELETE+INSERT).
    sqlx::query("DELETE FROM admin_role_permissions WHERE role_id::text = $1")
        .bind(&moderator_id)
        .execute(&pool)
        .await
        .unwrap();
    sqlx::query("INSERT INTO admin_role_permissions (role_id, permission_key) VALUES ($1::uuid, 'users.read')")
        .bind(&moderator_id)
        .execute(&pool)
        .await
        .unwrap();

    // Second boot: idempotent. Row counts stable, role IDs stable (upsert not
    // delete+recreate), and the hand-edited permission set is reset.
    rbac::seed_permissions_and_roles(&pool).await.expect("second seed");

    let perm_count_2: i64 = sqlx::query_scalar("SELECT COUNT(*) FROM admin_permissions").fetch_one(&pool).await.unwrap();
    assert_eq!(perm_count_2, perm_count);
    let role_count_2: i64 = sqlx::query_scalar("SELECT COUNT(*) FROM admin_roles").fetch_one(&pool).await.unwrap();
    assert_eq!(role_count_2, role_count);

    let superadmin_id_2: String =
        sqlx::query_scalar("SELECT id::text FROM admin_roles WHERE key = 'superadmin'").fetch_one(&pool).await.unwrap();
    assert_eq!(superadmin_id_2, superadmin_id, "re-seeding must upsert in place, not recreate role rows");

    let moderator_perms: Vec<String> =
        sqlx::query_scalar("SELECT permission_key FROM admin_role_permissions WHERE role_id::text = $1 ORDER BY permission_key")
            .bind(&moderator_id)
            .fetch_all(&pool)
            .await
            .unwrap();
    let mut want_moderator: Vec<String> = SYSTEM_ROLES
        .iter()
        .find(|r| r.key == "moderator")
        .unwrap()
        .permissions
        .iter()
        .map(|s| s.to_string())
        .collect();
    want_moderator.sort();
    assert_eq!(moderator_perms, want_moderator, "hand-edited permission set must be reset by re-seed");

    // superadmin never gets explicit admin_role_permissions rows.
    let superadmin_perm_rows: i64 = sqlx::query_scalar("SELECT COUNT(*) FROM admin_role_permissions WHERE role_id::text = $1")
        .bind(&superadmin_id)
        .fetch_one(&pool)
        .await
        .unwrap();
    assert_eq!(superadmin_perm_rows, 0);
}
