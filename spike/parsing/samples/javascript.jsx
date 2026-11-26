import React from 'react';
export function greet(name) { return `Hello ${name}`; }
export const Card = ({ name }) => <article>{name}</article>;
export class Store { get() { return greet('world'); } }
