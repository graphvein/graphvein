import express from 'express';
import DefaultService, { Service as Alias } from './service';
import { Service } from './barrel';
import * as ns from './service';
import { nested } from './barrel';
import * as rejected from './rejections';
import Anonymous from './anonymous';
import type { Service as TypeOnly } from './service';

class Local { static run() {} }
Local.run();
Anonymous.run();
TypeOnly.plain();
export function invoke() {
  Alias.plain();
  Alias.quoted();
  DefaultService.run();
  Service.overloaded('text');
  Service.asynchronous();
  Service.generator();
  Service.plain?.();
  Service?.plain();
  ns.Service.plain();
  nested.Service.plain();
  Service.send('/t012f/wrapped');
  rejected.Own.direct();
  rejected.Replay.run();
}

// Top-level rejections exercise the additive path, without replacing legacy
// method-name matching inside functions.
rejected.Child.inherited();
rejected.Instance.run();
rejected.Accessor.run();
rejected.Field.run();
rejected.FunctionField.run();
rejected.Private.#run();
rejected.Repeated.run();
rejected.Merged.run();
rejected.Assigned.run();
rejected.Assigned.safe();
rejected.Defined.run();
rejected.DefinedMany.run();
rejected.AssignedMany.run();
rejected.Deleted.run();
rejected.Updated.run();
rejected.DynamicMutation.run();
rejected.Ambient.run();
rejected.Expression.run();
rejected.Computed.run();
rejected.StaticThis.run();
Service['plain']();
declare const method: string;
Service[method]();
export function shadow(Service: unknown) { Service.plain(); }
export function blockShadow() { { Service.plain(); const Service = {}; } }
export function instance() { new Service().plain(); }
export function caught() { try {} catch (Service) { Service.plain(); } }
export function looped() { for (const Service of []) { Service.plain(); } }
export function hoisted() { Service.plain(); { var Service; } }

const app = express();
function handler(req, res) { res.send('ok'); }
app.get('/t012f/wrapped', handler);
