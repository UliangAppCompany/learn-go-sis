-- name: GetStudent :one
select tenant_id, id, name, gpa from students 
where tenant_id =? and id=?; 

-- name: ListStudents :many 
select tenant_id, id,name,gpa from students 
where tenant_id = ?
order by id; 

-- name: CreateStudent :exec 
insert into students (tenant_id, id, name, gpa) 
values (?, ?,?,?); 

-- name: GetUserByEmail :one 
select tenant_id, id, email, password_hash from users 
where email = ?; 

-- name: CreateUser :exec 
insert into users (tenant_id, id, email, password_hash)
values (?, ?, ?, ?); 

-- name: CreateSession :exec 
insert into sessions (token, tenant_id, user_id, created_at, expires_at) 
values (?,?,?,?, ?); 

-- name: GetSession :one 
select token, tenant_id, user_id, created_at, expires_at from sessions
where token = ?; 

-- name: DeleteSession :exec 
delete from sessions where token = ?; 

-- name: DeleteExpiredSessions :exec
delete from sessions where expires_at <= ?; 
