-- LUX Indexer Database Initialization
-- Creates databases for all chain indexers

-- P-Chain (Platform)
CREATE DATABASE explorer_pchain;
GRANT ALL PRIVILEGES ON DATABASE explorer_pchain TO blockscout;

-- X-Chain (Exchange)
CREATE DATABASE explorer_xchain;
GRANT ALL PRIVILEGES ON DATABASE explorer_xchain TO blockscout;

-- A-Chain (AI)
CREATE DATABASE explorer_achain;
GRANT ALL PRIVILEGES ON DATABASE explorer_achain TO blockscout;

-- B-Chain (Bridge)
CREATE DATABASE explorer_bchain;
GRANT ALL PRIVILEGES ON DATABASE explorer_bchain TO blockscout;

-- Q-Chain (Quantum)
CREATE DATABASE explorer_qchain;
GRANT ALL PRIVILEGES ON DATABASE explorer_qchain TO blockscout;

-- T-Chain (Teleport)
CREATE DATABASE explorer_tchain;
GRANT ALL PRIVILEGES ON DATABASE explorer_tchain TO blockscout;

-- Z-Chain (Privacy)
CREATE DATABASE explorer_zchain;
GRANT ALL PRIVILEGES ON DATABASE explorer_zchain TO blockscout;

-- Testnet databases
CREATE DATABASE explorer_pchain_test;
GRANT ALL PRIVILEGES ON DATABASE explorer_pchain_test TO blockscout;

CREATE DATABASE explorer_xchain_test;
GRANT ALL PRIVILEGES ON DATABASE explorer_xchain_test TO blockscout;

CREATE DATABASE explorer_achain_test;
GRANT ALL PRIVILEGES ON DATABASE explorer_achain_test TO blockscout;

CREATE DATABASE explorer_bchain_test;
GRANT ALL PRIVILEGES ON DATABASE explorer_bchain_test TO blockscout;

CREATE DATABASE explorer_qchain_test;
GRANT ALL PRIVILEGES ON DATABASE explorer_qchain_test TO blockscout;

CREATE DATABASE explorer_tchain_test;
GRANT ALL PRIVILEGES ON DATABASE explorer_tchain_test TO blockscout;

CREATE DATABASE explorer_zchain_test;
GRANT ALL PRIVILEGES ON DATABASE explorer_zchain_test TO blockscout;
