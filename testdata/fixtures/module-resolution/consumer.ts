import primary, { a, renamed, Default, ns } from './barrel';
import { x, relayed } from './relay';
import relayDefault from './relay';
import hiddenDefault from './star';
import { clash as conflict } from './outer-conflict';
import { clash as override, b as localOverride } from './override';
import { cyclic } from './cycle-a';
import { work as esm } from './esm.js';
import { work as condition } from '@condition';
import { work as fallback } from '@fallback';
import { work as sub } from 'subpkg/part';
import { work as duplicate } from '@dup';
import { work as alias } from '@alias/tool';
import { work as prefix } from '@pkg';
import { work as base } from 'baseOnly';
import { work as fallen } from 'fallen';
import { work as baseFirst } from '@base';
import { work as fallWork } from '@fall-work';
import { a as same } from './conflict';
import * as starNamespace from './barrel';
import { repeated } from './repeated-barrel';
import { work as jsxSibling } from './jsx-sibling.jsx';
import { work as jsFallback } from './js-fallback.js';
import { a as typeOnly } from './type-only';
export function named() { a(); renamed(); }
export function defaults() { primary(); Default(); relayDefault(); }
export function namespace() { ns.a(); }
export function relays() { x(); relayed(); }
export function ambiguous() { conflict(); }
export function overrideExplicit() { override(); }
export function overrideLocal() { localOverride(); }
export function excludedDefault() { hiddenDefault(); }
export function cycle() { cyclic(); }
export function jsSubstitution() { esm(); }
export function conditions() { condition(); }
export function sourceFallback() { fallback(); }
export function subpath() { sub(); }
export function duplicates() { duplicate(); }
export function pathsAlias() { alias(); }
export function pathsFirst() { prefix(); }
export function baseOnly() { base(); }
export function failedPathsBase() { fallen(); }
export function baseBeforeWorkspace() { baseFirst(); }
export function failedPathsWorkspace() { fallWork(); }
export function starAgreement() { same(); }
export function nestedNamespace() { starNamespace.ns.b(); }
export function repeatedAmbiguity() { repeated(); }
export function jsxSubstitution() { jsxSibling(); }
export function javascriptFallback() { jsFallback(); }
export function typeOnlyUnresolved() { typeOnly(); }
