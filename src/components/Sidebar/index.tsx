/* eslint-disable @typescript-eslint/explicit-function-return-type */
import { FiGithub, FiInstagram, FiLinkedin, FiTwitter } from 'react-icons/fi';
import styles from './styles.module.scss';

export const Sidebar = (): JSX.Element => {
  return (
    <aside className={styles.aside}>
      <ul>
        <li className="">
          <a href="https://github.com/0xb0b1" target="_blank" rel="noreferrer">
            <FiGithub size={22} />
          </a>
        </li>
        <li>
          <a
            href="https://twitter.com/p_vcent"
            target="_blank"
            rel="noreferrer"
          >
            <FiTwitter size={22} />
          </a>
        </li>
        <li>
          <a
            href="https://www.linkedin.com/in/paulo-vicente-6abab0198/"
            target="_blank"
            rel="noreferrer"
          >
            <FiLinkedin size={22} />
          </a>
        </li>
        <li>
          <a
            href="https://www.instagram.com/p_vcent/"
            target="_blank"
            rel="noreferrer"
          >
            <FiInstagram size={22} />
          </a>
        </li>
      </ul>
    </aside>
  );
};
