-- name: GetStudent :one
select tenant_id, id, name, gpa from students
where tenant_id = $1 and id = $2;

-- name: ListStudents :many
select tenant_id, id, name, gpa from students
where tenant_id = $1
order by id;

-- name: CreateStudent :exec
insert into students (tenant_id, id, name, gpa)
values ($1, $2, $3, $4);

-- name: GetUserByEmail :one
select tenant_id, id, email, password_hash from users
where email = $1;

-- name: CreateUser :exec
insert into users (tenant_id, id, email, password_hash)
values ($1, $2, $3, $4);

-- name: CreateSession :exec
insert into sessions (token, tenant_id, user_id, created_at, expires_at)
values ($1, $2, $3, $4, $5);

-- name: GetSession :one
select token, tenant_id, user_id, created_at, expires_at from sessions
where token = $1;

-- name: DeleteSession :exec
delete from sessions where token = $1;

-- name: DeleteExpiredSessions :exec
delete from sessions where expires_at <= $1;
