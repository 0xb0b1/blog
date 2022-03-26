import { useState } from 'react';
import { BsFillMoonFill, BsFillSunFill } from 'react-icons/bs';
import styles from './header.module.scss';

export default function Header(): JSX.Element {
  const [darkTheme, setDarkTheme] = useState(false);
  return (
    <header className={styles.container}>
      <div className={styles.content}>
        <img src="/logo.jpg" alt="Logo" />
        <h2>Paulo Vicente</h2>
        <p>Getting to know yourself by knowing the world</p>
      </div>

      <button type="button" onClick={() => setDarkTheme(!darkTheme)}>
        {darkTheme ? <BsFillMoonFill size={28} /> : <BsFillSunFill size={28} />}
      </button>
    </header>
  );
}
