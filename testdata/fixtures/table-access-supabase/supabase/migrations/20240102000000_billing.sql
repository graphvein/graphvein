create schema billing;

create table billing.invoices (
  id bigint primary key,
  subscription_id text references public.subscriptions (id)
);
