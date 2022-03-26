/* eslint-disable @typescript-eslint/explicit-function-return-type */
import { useState } from 'react';
import {
  FiGithub,
  FiInstagram,
  FiLinkedin,
  FiMoon,
  FiTwitter,
} from 'react-icons/fi';
import { BsFillMoonFill, BsFillSunFill } from 'react-icons/bs';
import styles from './styles.module.scss';

export const Sidebar = (): JSX.Element => {
  const [darkTheme, setDarkTheme] = useState(false);

  return (
    <aside className={styles.aside}>
      <ul>
        <li className="">
          <FiGithub size={22} />
          <span className="">Github</span>
        </li>
        <li>
          <FiTwitter size={22} />
          <span>Twitter</span>
        </li>
        <li>
          <FiLinkedin size={22} />
          <span>Linkedin</span>
        </li>
        <li>
          <FiInstagram size={22} />
          <span>Instagram</span>
        </li>
      </ul>

      <button type="button" onClick={() => setDarkTheme(!darkTheme)}>
        {darkTheme ? <BsFillMoonFill size={28} /> : <BsFillSunFill size={28} />}
      </button>
    </aside>
  );
};
