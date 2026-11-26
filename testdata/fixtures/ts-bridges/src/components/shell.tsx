import { Component, type ReactNode } from 'react';

// A class component: JR targets may be classes.
export class Shell extends Component<{ children?: ReactNode }> {
  render() {
    return <section>{this.props.children}</section>;
  }
}
