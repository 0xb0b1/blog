import styles from './header.module.scss';

export default function Header(): JSX.Element {
  return (
    <header className={styles.container}>
      <div className={styles.content}>
        <img src="/logo.jpg" alt="Logo" />
        <h2>Paulo Vicente</h2>
        <p>Getting to know yourself by knowing the world</p>
      </div>
    </header>
  );
}
