create table students (
  tenant_id   text not null,  
  id          integer not null,
  name        text not null,  
  gpa         real not null, 
  primary key (tenant_id, id)
); 


create table users (
  tenant_id   text      not null, 
  id          integer   not null, 
  email       text      not null unique, -- email -> tenant 
  password_hash text    not null, 
  primary key (tenant_id, id)
); 

create table sessions (
  token       text        not null primary key, 
  tenant_id   text        not null, 
  user_id     integer     not null, 
  created_at  text        not null, 
  expires_at  text        not null
);


