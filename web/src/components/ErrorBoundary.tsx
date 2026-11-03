import { Component, type ReactNode } from "react";

// One broken screen must not blank the whole app; show a recoverable message instead.
export class ErrorBoundary extends Component<{ children: ReactNode; resetKey?: string }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() {
    return { failed: true };
  }
  componentDidUpdate(prev: { resetKey?: string }) {
    if (this.state.failed && prev.resetKey !== this.props.resetKey) this.setState({ failed: false });
  }
  render() {
    if (!this.state.failed) return this.props.children;
    return (
      <div className="card empty" role="alert">
        <b>Something went wrong on this screen</b>
        <p>Your records are safe. Try again, or open another section.</p>
        <button className="btn" onClick={() => this.setState({ failed: false })}>Try again</button>
      </div>
    );
  }
}
