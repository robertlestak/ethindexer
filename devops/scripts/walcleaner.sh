#!/bin/bash
set -ev

SLOT_NAME="replication_slot_slave1"

cat > /tmp/wal-clean-query.sql <<EOF
SELECT 
       lpad((pg_control_checkpoint()).timeline_id::text, 8, '0') ||
       lpad(split_part(restart_lsn::text, '/', 1), 8, '0') ||
       lpad(substr(split_part(restart_lsn::text, '/', 2), 1, 2), 8, '0')
       AS wal_file
FROM pg_replication_slots
WHERE slot_name = '$SLOT_NAME';
EOF

export PGPASSWORD=$POSTGRES_PASSWORD

LAST_WAL_FILE=`psql \
    -d $POSTGRES_DB \
    -U $POSTGRES_USER \
    -h $DB_HOST \
    -p $DB_PORT \
    -t \
    -c "$(cat /tmp/wal-clean-query.sql)"`


echo "Last WAL file: $LAST_WAL_FILE"

pg_archivecleanup -d /master/wal_archive $LAST_WAL_FILE