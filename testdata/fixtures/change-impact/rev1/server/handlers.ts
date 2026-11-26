import { loadItems } from './items';

export function listItems(req: any, res: any) {
  res.json(loadItems());
}

export function getItem(req: any, res: any) {
  res.json({ id: req.params.id });
}

export function dropLegacy(req: any, res: any) {
  res.sendStatus(204);
}

export function report(req: any, res: any) {
  res.json({});
}
