/* eslint-disable @typescript-eslint/explicit-function-return-type */
import { FiGithub, FiInstagram, FiLinkedin, FiTwitter } from 'react-icons/fi';
import styles from './styles.module.scss';

export const Sidebar = (): JSX.Element => {
  return (
    <aside className={styles.aside}>
      <ul>
        <li className="">
          <FiGithub size={22} />
          {/* <span className="">Github</span> */}
        </li>
        <li>
          <FiTwitter size={22} />
          {/* <span>Twitter</span> */}
        </li>
        <li>
          <FiLinkedin size={22} />
          {/* <span>Linkedin</span> */}
        </li>
        <li>
          <FiInstagram size={22} />
          {/* <span>Instagram</span> */}
        </li>
      </ul>
    </aside>
  );
};
