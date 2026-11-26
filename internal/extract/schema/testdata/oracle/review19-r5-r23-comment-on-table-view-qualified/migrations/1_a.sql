CREATE TABLE public.t (a int);
CREATE VIEW public.v AS SELECT a FROM public.t;
COMMENT ON TABLE public.v IS 'x';
