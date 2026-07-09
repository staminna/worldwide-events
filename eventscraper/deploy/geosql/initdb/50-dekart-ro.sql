-- Read-only role Dekart uses to query event data. Runs once on first DB init
-- (as the eventscraper owner), so ALTER DEFAULT PRIVILEGES covers the tables the
-- app creates later.
DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'dekart_ro') THEN
    CREATE ROLE dekart_ro WITH LOGIN PASSWORD 'dekart_ro';
  END IF;
END
$$;

GRANT CONNECT ON DATABASE eventscraper TO dekart_ro;
GRANT USAGE ON SCHEMA public TO dekart_ro;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO dekart_ro;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO dekart_ro;
