# Separate databases: FreeRADIUS (MySQL) + backend (Postgres)

FreeRADIUS uses MySQL (the most stable and best-documented option for FreeRADIUS), while the Go DDD backend uses Postgres per its architectural convention. The backend opens connections to two databases. Rejected alternatives: a single Postgres for both (FreeRADIUS + Postgres support is less battle-tested) and a single MySQL for everything (deviates from the backend's Postgres convention).
