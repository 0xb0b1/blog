/* eslint-disable no-console */
/* eslint-disable @typescript-eslint/explicit-module-boundary-types */
/* eslint-disable @typescript-eslint/explicit-function-return-type */
import { GetStaticProps } from 'next';
import { FiCalendar, FiUser } from 'react-icons/fi';
import Prismic from '@prismicio/client';
import { RichText } from 'prismic-dom';
import { Head } from 'next/document';
import Header from '../components/Header';

import { getPrismicClient } from '../services/prismic';

import commonStyles from '../styles/common.module.scss';
import styles from './home.module.scss';

type Post = {
  slug: string;
  author: string;
  title: string;
  excerpt: string;
  updatedAt: string;
};

interface PostsProps {
  posts: Post[];
}

interface PostPagination {
  next_page: string;
  results: Post[];
}

interface HomeProps {
  postsPagination: PostPagination;
}

export default function Home({ posts }: PostsProps): JSX.Element {
  // console.log(posts);
  return (
    <>
      <main className={styles.postListContainer}>
        <Header />

        <section className={styles.postListContent}>
          {posts.map(post => (
            <a key={post.slug} href="/">
              <strong>{post.title}</strong>
              <p>{post.excerpt}.</p>
              <ul>
                <li>
                  <FiCalendar />
                  {post.updatedAt}
                </li>
                <li>
                  <FiUser />
                  {post.author}
                </li>
              </ul>
            </a>
          ))}
        </section>

        <button type="button">Carregar mais posts</button>
      </main>
    </>
  );
}

export const getStaticProps: GetStaticProps = async () => {
  const prismic = getPrismicClient();
  const postsResponse = await prismic.query(
    [Prismic.predicates.at('document.type', 'post')],
    {
      fetch: ['post.title', 'post.subtitle', 'post.content', 'post.author'],
      pageSize: 10,
    }
  );

  console.log(postsResponse.results);

  const posts = postsResponse.results.map(post => {
    return {
      slug: post.uid,
      author: RichText.asText(post.data.author),
      title: RichText.asText(post.data.title),
      excerpt: RichText.asText(post.data.content.splice(0, 3)),
      updatedAt: new Date(post.last_publication_date).toLocaleDateString(
        'pt-BR',
        {
          day: '2-digit',
          month: 'long',
          year: 'numeric',
        }
      ),
    };
  });

  return {
    props: { posts },
  };
};
