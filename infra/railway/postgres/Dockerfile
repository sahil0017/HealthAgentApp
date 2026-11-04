# pgvector Postgres with the schema bootstrap baked in (Railway cannot mount init scripts). Copy of infra/postgres/init.sql.
FROM pgvector/pgvector:pg16
COPY init.sql /docker-entrypoint-initdb.d/001-init.sql
