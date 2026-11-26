import { useMutation } from '@tanstack/react-query';
import { createFileRoute } from '@tanstack/react-router';

async function createItem() {
  return null;
}

function render(x: number) {
  return x;
}

function tick() {}

function ItemsTableContent() {
  return <table />;
}

function Items() {
  const mutation = useMutation({ mutationFn: createItem });
  const handleClick = () => mutation.mutate();
  setTimeout(tick, 10);
  [1, 2].map(render);
  return (
    <div onClick={handleClick}>
      <ItemsTableContent />
    </div>
  );
}

// PF2 from module scope: the file passes Items as a route component.
export const Route = createFileRoute('/items')({ component: Items });

function List({ render }: { render: (x: number) => number }) {
  // render is the parameter here: shadowed, D.
  return [1].map(render);
}

export function Callbacks() {
  // The callback's own parameter shadows ItemsTableContent (D).
  return [ItemsTableContent].map((ItemsTableContent) => <ItemsTableContent />).concat(List({ render }));
}
