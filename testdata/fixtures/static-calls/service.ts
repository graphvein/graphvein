export class Service {
  static plain() { return 1; }
  static 'quoted'() { return 1; }
  static overloaded(value: string): string;
  static overloaded(value: number): number;
  static overloaded(value: string | number) { return value; }
  static async asynchronous() { return 1; }
  static *generator() { yield 1; }
  static send(url: string) { return fetch(url); }
}
export default class DefaultService {
  static run() { return 2; }
}
