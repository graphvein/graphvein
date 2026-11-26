create table public.profiles (
  id uuid primary key,
  username text
);

create table public.products (
  id text primary key,
  name text
);

create table public.prices (
  id text primary key,
  product_id text references public.products (id),
  amount bigint
);

create table public.subscriptions (
  id text primary key,
  profile_id uuid references public.profiles (id),
  price_id text references public.prices (id)
);

create table public.tags (
  id bigint primary key,
  label text
);

create table public.product_tags (
  product_id text references public.products (id),
  tag_id bigint references public.tags (id),
  primary key (product_id, tag_id)
);

create table public.messages (
  id bigint primary key,
  sender_id uuid references public.profiles (id),
  recipient_id uuid references public.profiles (id),
  body text
);
