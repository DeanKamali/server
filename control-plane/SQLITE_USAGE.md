# SQLite Usage Guide

## Why SQLite?

SQLite is perfect for **local testing and development**:

✅ **No setup required** - Just run the control plane  
✅ **File-based** - Database stored in a single file  
✅ **No server needed** - No PostgreSQL installation required  
✅ **Fast** - Great performance for local development  
✅ **Portable** - Easy to backup/restore (just copy the file)

## Usage

### Default (SQLite)

Just run the control plane - SQLite is the default:

```bash
cd control-plane
./control-plane -port 8080
```

This creates `./control_plane.db` automatically.

### Custom SQLite Path

```bash
./control-plane \
  -port 8080 \
  -db-type sqlite \
  -db-dsn "/path/to/my_control_plane.db"
```

### Using Startup Script

The startup script uses SQLite by default:

```bash
./start_control_plane.sh
```

## Database File Location

- **Default**: `./control_plane.db` (in the control-plane directory)
- **Custom**: Specify with `-db-dsn` flag

## Switching to PostgreSQL

For production or multi-instance deployments, use PostgreSQL:

```bash
./control-plane \
  -port 8080 \
  -db-type postgres \
  -db-dsn "postgres://user:pass@localhost:5432/control_plane?sslmode=disable"
```

## Comparison

| Feature | SQLite | PostgreSQL |
|---------|--------|------------|
| **Setup** | None | Requires installation |
| **Server** | No | Yes |
| **File-based** | ✅ Yes | ❌ No |
| **Multi-instance** | ❌ No | ✅ Yes |
| **Production** | ⚠️ Not recommended | ✅ Recommended |
| **Testing** | ✅ Perfect | ✅ Good |

## When to Use SQLite

✅ **Use SQLite for:**
- Local development
- Testing
- Single-instance deployments
- Quick prototyping

❌ **Don't use SQLite for:**
- Production (use PostgreSQL)
- Multi-instance deployments
- High-concurrency scenarios

## Backup

SQLite database is just a file - easy to backup:

```bash
# Backup
cp control_plane.db control_plane.db.backup

# Restore
cp control_plane.db.backup control_plane.db
```

## Migration from SQLite to PostgreSQL

1. Export data from SQLite (if needed)
2. Start control plane with PostgreSQL
3. Recreate projects/compute nodes via API

Or use a migration tool to transfer data.



