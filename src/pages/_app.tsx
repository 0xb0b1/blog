import { AppProps } from 'next/app';
import { Sidebar } from '../components/Sidebar';
import '../styles/globals.scss';

function MyApp({ Component, pageProps }: AppProps): JSX.Element {
  return (
    <>
      <Sidebar />
      <Component {...pageProps} />
    </>
  );
}

export default MyApp;
