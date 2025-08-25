# Have to include the init script this way because Docker did NOT want to accept the init file as a mount binding
FROM postgres:16-alpine
COPY init/*.sql /docker-entrypoint-initdb.d/